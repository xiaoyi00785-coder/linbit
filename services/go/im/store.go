package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Store wraps the database handle and offers typed accessors used by handlers.
// Every query on it runs through database/sql; no ORM.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store over the given handle.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ---- conversation ----

// Conversation mirrors a row of im_conversations with its last message summary.
type Conversation struct {
	ID        int64
	Kind      string
	PeerLo    sql.NullString
	PeerHi    sql.NullString
	GroupID   sql.NullInt64
	LastMsgID sql.NullInt64
	LastMsgAt sql.NullTime
	UpdatedAt time.Time
}

// upsertC2C returns (and creates if missing) the conversation for a 1v1 pair.
// The pair is stored in canonical (lo, hi) order so either order of the two
// uids reaches the same row.
func (s *Store) upsertC2C(ctx context.Context, a, b string) (*Conversation, error) {
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	var c Conversation
	err := s.db.QueryRowContext(ctx, `
        INSERT INTO im_conversations (kind, peer_lo, peer_hi)
        VALUES ('c2c', $1, $2)
        ON CONFLICT (peer_lo, peer_hi) WHERE kind = 'c2c' DO UPDATE SET updated_at = now()
        RETURNING id, kind, peer_lo, peer_hi, group_id, last_msg_id, last_msg_at, updated_at`,
		lo, hi).Scan(&c.ID, &c.Kind, &c.PeerLo, &c.PeerHi, &c.GroupID, &c.LastMsgID, &c.LastMsgAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	// Ensure both sides are listed as members so unread counters work.
	for _, uid := range []string{a, b} {
		if _, err := s.db.ExecContext(ctx, `
            INSERT INTO im_conversation_members (conversation_id, uid) VALUES ($1, $2)
            ON CONFLICT DO NOTHING`, c.ID, uid); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// conversationForGroup returns (creating if missing) the conversation attached
// to a group. Called from group creation to keep the mapping 1:1.
func (s *Store) conversationForGroup(ctx context.Context, groupID int64) (*Conversation, error) {
	var c Conversation
	err := s.db.QueryRowContext(ctx, `
        INSERT INTO im_conversations (kind, group_id)
        VALUES ('group', $1)
        ON CONFLICT (group_id) WHERE kind = 'group' DO UPDATE SET updated_at = now()
        RETURNING id, kind, peer_lo, peer_hi, group_id, last_msg_id, last_msg_at, updated_at`,
		groupID).Scan(&c.ID, &c.Kind, &c.PeerLo, &c.PeerHi, &c.GroupID, &c.LastMsgID, &c.LastMsgAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE im_groups SET conversation_id=$1 WHERE id=$2 AND conversation_id IS NULL`, c.ID, groupID)
	return &c, err
}

// listConversations returns the given user's conversations newest first.
func (s *Store) listConversations(ctx context.Context, uid string, limit int) ([]Conversation, []int, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT c.id, c.kind, c.peer_lo, c.peer_hi, c.group_id, c.last_msg_id, c.last_msg_at, c.updated_at,
               m.unread
        FROM im_conversations c
        JOIN im_conversation_members m ON m.conversation_id = c.id
        WHERE m.uid = $1 AND m.hidden = false
        ORDER BY COALESCE(c.last_msg_at, c.updated_at) DESC
        LIMIT $2`, uid, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []Conversation
	var unread []int
	for rows.Next() {
		var c Conversation
		var u int
		if err := rows.Scan(&c.ID, &c.Kind, &c.PeerLo, &c.PeerHi, &c.GroupID, &c.LastMsgID, &c.LastMsgAt, &c.UpdatedAt, &u); err != nil {
			return nil, nil, err
		}
		out = append(out, c)
		unread = append(unread, u)
	}
	return out, unread, rows.Err()
}

// ---- messages ----

// Message mirrors a row of im_messages.
type Message struct {
	ID             int64
	ConversationID int64
	Seq            int64
	SenderUID      string
	Kind           string
	Payload        []byte
	Mentions       []string
	ReplyTo        sql.NullInt64
	MsgRandom      int64
	Recalled       bool
	CreatedAt      time.Time
}

// insertMessage allocates the next seq for the conversation and inserts the
// row atomically. It returns the stored Message.
func (s *Store) insertMessage(ctx context.Context, convID int64, senderUID, kind string, payload []byte, mentions []string, replyTo sql.NullInt64, msgRandom int64) (*Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	var seq int64
	if err := tx.QueryRowContext(ctx, `
        SELECT COALESCE(MAX(seq), 0) + 1 FROM im_messages WHERE conversation_id = $1`, convID).Scan(&seq); err != nil {
		return nil, err
	}
	var m Message
	m.ConversationID = convID
	m.Seq = seq
	m.SenderUID = senderUID
	m.Kind = kind
	m.Payload = payload
	m.Mentions = mentions
	m.ReplyTo = replyTo
	m.MsgRandom = msgRandom
	mb, err := json.Marshal(stringSlice(mentions))
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, `
        INSERT INTO im_messages (conversation_id, seq, sender_uid, kind, payload, mentions, reply_to, msg_random)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING id, created_at`,
		convID, seq, senderUID, kind, payload, mb, replyTo, msgRandom).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE im_conversations SET last_msg_id = $1, last_msg_at = $2, updated_at = now()
        WHERE id = $3`, m.ID, m.CreatedAt, convID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE im_conversation_members SET unread = unread + 1
        WHERE conversation_id = $1 AND uid <> $2`, convID, senderUID); err != nil {
		return nil, err
	}
	return &m, tx.Commit()
}

// listMessages returns up to limit messages for a conversation, newest first,
// before (strictly) the given seq. Pass seq=0 to start at the newest.
func (s *Store) listMessages(ctx context.Context, convID int64, beforeSeq int64, limit int) ([]Message, error) {
	if beforeSeq <= 0 {
		beforeSeq = 1 << 62
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT id, conversation_id, seq, sender_uid, kind, payload, mentions, reply_to, msg_random, recalled, created_at
        FROM im_messages
        WHERE conversation_id = $1 AND seq < $2
        ORDER BY seq DESC
        LIMIT $3`, convID, beforeSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var ment []byte
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.Seq, &m.SenderUID, &m.Kind, &m.Payload,
			&ment, &m.ReplyTo, &m.MsgRandom, &m.Recalled, &m.CreatedAt); err != nil {
			return nil, err
		}
		if len(ment) > 0 {
			_ = json.Unmarshal(ment, &m.Mentions)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// getMessage returns the row with the given id, or sql.ErrNoRows if missing.
func (s *Store) getMessage(ctx context.Context, id int64) (*Message, error) {
	var m Message
	var ment []byte
	err := s.db.QueryRowContext(ctx, `
        SELECT id, conversation_id, seq, sender_uid, kind, payload, mentions, reply_to, msg_random, recalled, created_at
        FROM im_messages WHERE id = $1`, id).Scan(
		&m.ID, &m.ConversationID, &m.Seq, &m.SenderUID, &m.Kind, &m.Payload,
		&ment, &m.ReplyTo, &m.MsgRandom, &m.Recalled, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(ment) > 0 {
		_ = json.Unmarshal(ment, &m.Mentions)
	}
	return &m, nil
}

// stringSlice returns s or an empty slice (so json.Marshal emits [] not null).
func stringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// recallMessage marks a message as recalled; callers enforce the time window.
func (s *Store) recallMessage(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE im_messages SET recalled = true, recalled_at = now() WHERE id = $1 AND recalled = false`, id)
	return err
}

// markRead advances the member's last_read_seq and clears unread.
func (s *Store) markRead(ctx context.Context, convID int64, uid string, seq int64) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE im_conversation_members
        SET last_read_seq = GREATEST(last_read_seq, $3), unread = GREATEST(unread - (
            SELECT COUNT(*) FROM im_messages
            WHERE conversation_id = $1 AND seq > last_read_seq AND seq <= $3), 0)
        WHERE conversation_id = $1 AND uid = $2`, convID, uid, seq)
	return err
}

// ---- groups ----

// Group mirrors a row of im_groups.
type Group struct {
	ID             int64
	Name           string
	Avatar         sql.NullString
	Announcement   string
	AnnouncementAt sql.NullTime
	OwnerUID       string
	MaxMembers     int
	MuteAll        bool
	MemberCount    int
	ConversationID sql.NullInt64
	CreatedAt      time.Time
}

func (s *Store) createGroup(ctx context.Context, name, ownerUID string, avatar string, maxMembers int) (*Group, error) {
	var g Group
	err := s.db.QueryRowContext(ctx, `
        INSERT INTO im_groups (name, avatar, owner_uid, max_members, member_count)
        VALUES ($1, NULLIF($2, ''), $3, $4, 1)
        RETURNING id, name, avatar, announcement, announcement_at, owner_uid, max_members, mute_all, member_count, conversation_id, created_at`,
		name, avatar, ownerUID, maxMembers).Scan(
		&g.ID, &g.Name, &g.Avatar, &g.Announcement, &g.AnnouncementAt, &g.OwnerUID, &g.MaxMembers,
		&g.MuteAll, &g.MemberCount, &g.ConversationID, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `
        INSERT INTO im_group_members (group_id, uid, role) VALUES ($1, $2, 'owner')`,
		g.ID, ownerUID); err != nil {
		return nil, err
	}
	conv, err := s.conversationForGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `
        INSERT INTO im_conversation_members (conversation_id, uid) VALUES ($1, $2)
        ON CONFLICT DO NOTHING`, conv.ID, ownerUID); err != nil {
		return nil, err
	}
	g.ConversationID = sql.NullInt64{Int64: conv.ID, Valid: true}
	return &g, nil
}

func (s *Store) getGroup(ctx context.Context, id int64) (*Group, error) {
	var g Group
	err := s.db.QueryRowContext(ctx, `
        SELECT id, name, avatar, announcement, announcement_at, owner_uid, max_members, mute_all, member_count, conversation_id, created_at
        FROM im_groups WHERE id = $1`, id).Scan(
		&g.ID, &g.Name, &g.Avatar, &g.Announcement, &g.AnnouncementAt, &g.OwnerUID, &g.MaxMembers,
		&g.MuteAll, &g.MemberCount, &g.ConversationID, &g.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// GroupMember mirrors a row of im_group_members.
type GroupMember struct {
	UID        string
	Role       string
	MutedUntil sql.NullTime
	Nickname   sql.NullString
	JoinedAt   time.Time
}

// getGroupMember returns the (group, uid) membership row, or sql.ErrNoRows.
func (s *Store) getGroupMember(ctx context.Context, groupID int64, uid string) (*GroupMember, error) {
	var m GroupMember
	err := s.db.QueryRowContext(ctx, `
        SELECT uid, role, muted_until, nickname, joined_at
        FROM im_group_members WHERE group_id = $1 AND uid = $2`, groupID, uid).Scan(
		&m.UID, &m.Role, &m.MutedUntil, &m.Nickname, &m.JoinedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) listGroupMembers(ctx context.Context, groupID int64, limit int) ([]GroupMember, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT uid, role, muted_until, nickname, joined_at
        FROM im_group_members WHERE group_id = $1
        ORDER BY CASE role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, joined_at
        LIMIT $2`, groupID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GroupMember
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.UID, &m.Role, &m.MutedUntil, &m.Nickname, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// addGroupMember inserts a new member (no-op on conflict) and keeps member_count
// in sync. It returns true when a new row was inserted.
func (s *Store) addGroupMember(ctx context.Context, groupID int64, uid, role string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
        INSERT INTO im_group_members (group_id, uid, role) VALUES ($1, $2, $3)
        ON CONFLICT DO NOTHING`, groupID, uid, role)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		if _, err := s.db.ExecContext(ctx, `UPDATE im_groups SET member_count = member_count + 1, updated_at = now() WHERE id = $1`, groupID); err != nil {
			return false, err
		}
		convID, err := s.groupConvID(ctx, groupID)
		if err != nil {
			return false, err
		}
		if convID > 0 {
			if _, err := s.db.ExecContext(ctx, `
                INSERT INTO im_conversation_members (conversation_id, uid) VALUES ($1, $2)
                ON CONFLICT DO NOTHING`, convID, uid); err != nil {
				return false, err
			}
		}
	}
	return n > 0, nil
}

// removeGroupMember removes the member and decrements member_count. It returns
// true when a row was removed.
func (s *Store) removeGroupMember(ctx context.Context, groupID int64, uid string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM im_group_members WHERE group_id = $1 AND uid = $2`, groupID, uid)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		if _, err := s.db.ExecContext(ctx, `UPDATE im_groups SET member_count = GREATEST(member_count - 1, 0), updated_at = now() WHERE id = $1`, groupID); err != nil {
			return false, err
		}
	}
	return n > 0, nil
}

// setRole updates a member's role (owner|admin|member); it does not allow
// changing the owner's role here — ownership transfer is a separate operation.
func (s *Store) setRole(ctx context.Context, groupID int64, uid, role string) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE im_group_members SET role = $3
        WHERE group_id = $1 AND uid = $2 AND role <> 'owner'`, groupID, uid, role)
	return err
}

// setMemberMute sets or clears a member's mute window; a zero until clears it.
func (s *Store) setMemberMute(ctx context.Context, groupID int64, uid string, until time.Time) error {
	if until.IsZero() {
		_, err := s.db.ExecContext(ctx, `UPDATE im_group_members SET muted_until = NULL WHERE group_id = $1 AND uid = $2`, groupID, uid)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE im_group_members SET muted_until = $3 WHERE group_id = $1 AND uid = $2`, groupID, uid, until)
	return err
}

func (s *Store) setGroupMuteAll(ctx context.Context, groupID int64, muted bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE im_groups SET mute_all = $2, updated_at = now() WHERE id = $1`, groupID, muted)
	return err
}

func (s *Store) setAnnouncement(ctx context.Context, groupID int64, by, text string) error {
	_, err := s.db.ExecContext(ctx, `
        UPDATE im_groups SET announcement = $2, announcement_by = $3, announcement_at = now(), updated_at = now()
        WHERE id = $1`, groupID, text, by)
	return err
}

func (s *Store) groupConvID(ctx context.Context, groupID int64) (int64, error) {
	var id sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT conversation_id FROM im_groups WHERE id = $1`, groupID).Scan(&id); err != nil {
		return 0, err
	}
	return id.Int64, nil
}

// ---- redpackets ----

// RedPacket mirrors a row of im_redpackets.
type RedPacket struct {
	ID              int64
	ConversationID  int64
	GroupID         sql.NullInt64
	SenderUID       string
	Asset           string
	TotalAmount     decimal.Decimal
	Shares          int
	RemainingShares int
	RemainingAmount decimal.Decimal
	Mode            string
	Blessing        string
	Status          string
	ExpiresAt       time.Time
	CreatedAt       time.Time
	MessageID       sql.NullInt64
}

func (s *Store) insertRedPacket(ctx context.Context, rp *RedPacket) error {
	group := sql.NullInt64{}
	if rp.GroupID.Valid {
		group = rp.GroupID
	}
	return s.db.QueryRowContext(ctx, `
        INSERT INTO im_redpackets (conversation_id, group_id, sender_uid, asset, total_amount, shares,
            remaining_shares, remaining_amount, mode, blessing, status, expires_at)
        VALUES ($1, $2, $3, $4, $5, $6, $6, $5, $7, $8, 'open', $9)
        RETURNING id, created_at`,
		rp.ConversationID, group, rp.SenderUID, rp.Asset, rp.TotalAmount.String(),
		rp.Shares, rp.Mode, rp.Blessing, rp.ExpiresAt,
	).Scan(&rp.ID, &rp.CreatedAt)
}

// getRedPacket loads a redpacket row for display.
func (s *Store) getRedPacket(ctx context.Context, id int64) (*RedPacket, error) {
	var rp RedPacket
	var total, remain string
	err := s.db.QueryRowContext(ctx, `
        SELECT id, conversation_id, group_id, sender_uid, asset, total_amount, shares,
               remaining_shares, remaining_amount, mode, blessing, status, expires_at, created_at, message_id
        FROM im_redpackets WHERE id = $1`, id).Scan(
		&rp.ID, &rp.ConversationID, &rp.GroupID, &rp.SenderUID, &rp.Asset, &total, &rp.Shares,
		&rp.RemainingShares, &remain, &rp.Mode, &rp.Blessing, &rp.Status, &rp.ExpiresAt, &rp.CreatedAt, &rp.MessageID)
	if err != nil {
		return nil, err
	}
	if rp.TotalAmount, err = decimal.NewFromString(total); err != nil {
		return nil, err
	}
	if rp.RemainingAmount, err = decimal.NewFromString(remain); err != nil {
		return nil, err
	}
	return &rp, nil
}

// RedPacketClaim mirrors a row of im_redpacket_claims.
type RedPacketClaim struct {
	UID       string
	Amount    decimal.Decimal
	ClaimedAt time.Time
	IsBest    bool
}

func (s *Store) listClaims(ctx context.Context, redpacketID int64) ([]RedPacketClaim, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT uid, amount, claimed_at, is_best
        FROM im_redpacket_claims WHERE redpacket_id = $1
        ORDER BY claimed_at ASC`, redpacketID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RedPacketClaim
	for rows.Next() {
		var c RedPacketClaim
		var amt string
		if err := rows.Scan(&c.UID, &amt, &c.ClaimedAt, &c.IsBest); err != nil {
			return nil, err
		}
		if c.Amount, err = decimal.NewFromString(amt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ErrNotFound is returned when a store lookup finds no row.
var ErrNotFound = errors.New("not found")

// touchPresence upserts the user's presence row.
func (s *Store) touchPresence(ctx context.Context, uid, platform string) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO im_presence (uid, last_seen_at, platform)
        VALUES ($1, now(), $2)
        ON CONFLICT (uid) DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at, platform = EXCLUDED.platform`,
		uid, platform)
	return err
}

func (s *Store) clearPresence(ctx context.Context, uid string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE im_presence SET platform = NULL WHERE uid = $1`, uid)
	return err
}
