package main

import "net/url"

// instClass is the public catalogue label for one symbol.
type instClass struct {
	Market string // spot | futures
	Kind   string // crypto | stock | metal | commodity
}

func categoryKind(id string) string {
	switch id {
	case "stocks":
		return "stock"
	case "metals":
		return "metal"
	case "commodities":
		return "commodity"
	default:
		return "crypto"
	}
}

func classOf(m map[string]instClass, symbol, defaultMarket string) instClass {
	if c, ok := m[symbol]; ok && c.Kind != "" {
		return c
	}
	if defaultMarket == "" {
		defaultMarket = "spot"
	}
	return instClass{Market: defaultMarket, Kind: "crypto"}
}

// allowInDefaultList decides whether a symbol appears in a public crypto list.
// A specific symbol, symbols= list, or category= request is handled by the caller
// and is not passed here. kind= is an explicit filter. include=tradfi adds the
// TradFi names back onto the default list.
func allowInDefaultList(kind string, q url.Values) bool {
	filter := q.Get("kind")
	if filter != "" {
		return kind == filter
	}
	if kind == "" || kind == "crypto" {
		return true
	}
	return q.Get("include") == "tradfi"
}

func validKind(k string) bool {
	switch k {
	case "", "crypto", "stock", "metal", "commodity":
		return true
	default:
		return false
	}
}
