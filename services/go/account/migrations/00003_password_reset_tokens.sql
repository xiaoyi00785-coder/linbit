-- +goose Up
-- Short-lived, single-use password-reset tokens. The forgot-password flow is
-- split into two steps: POST /auth/password/verify-code validates the emailed/
-- texted code and issues a resetToken (stored hashed here); POST
-- /auth/password/reset then consumes that token to set the new password. This
-- keeps the verification code out of the final reset call and bounds the window.
CREATE TABLE password_reset_tokens (
    id         BIGSERIAL PRIMARY KEY,
    token_hash TEXT NOT NULL,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    consumed   BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_password_reset_tokens_hash ON password_reset_tokens (token_hash);

-- +goose Down
DROP TABLE password_reset_tokens;
