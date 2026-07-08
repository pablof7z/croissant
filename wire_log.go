package main

import (
	"fmt"
	"strings"

	"fiatjaf.com/nostr"
)

const (
	clrReset   = "\033[0m"
	clrDim     = "\033[2m"
	clrRed     = "\033[31m"
	clrGreen   = "\033[32m"
	clrYellow  = "\033[33m"
	clrCyan    = "\033[36m"
	clrGray    = "\033[90m"
	clrBGreen  = "\033[92m"
	clrBYellow = "\033[93m"
	clrBBlue   = "\033[94m"
	clrBMagenta = "\033[95m"
	clrBCyan   = "\033[96m"
)

func wireTrunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func logWireMessage(ip, ua, message string) {
	envelope, err := nostr.ParseMessage(message)
	if err != nil {
		fmt.Printf("%s  %-6s%s  %-18s  %s\n",
			clrGray, "?", clrReset, ip, wireTrunc(message, 100))
		return
	}

	switch env := envelope.(type) {
	case *nostr.EventEnvelope:
		group := ""
		if t := env.Event.Tags.Find("h"); t != nil && len(t) >= 2 {
			group = fmt.Sprintf("  group=%q", t[1])
		}
		content := ""
		if c := env.Event.Content; c != "" {
			content = fmt.Sprintf("  %s%q%s", clrDim, wireTrunc(c, 60), clrReset)
		}
		fmt.Printf("%s→ EVENT %s  %-18s  kind=%-6d  id=%s  pub=%s%s%s\n",
			clrBGreen, clrReset,
			ip,
			env.Event.Kind,
			env.Event.ID.Hex()[:12],
			env.Event.PubKey.Hex()[:12],
			group,
			content,
		)

	case *nostr.ReqEnvelope:
		parts := make([]string, len(env.Filters))
		for i, f := range env.Filters {
			b, _ := f.MarshalJSON()
			parts[i] = string(b)
		}
		fmt.Printf("%s→ REQ   %s  %-18s  sub=%-16s  %s\n",
			clrBBlue, clrReset,
			ip,
			wireTrunc(env.SubscriptionID, 16),
			strings.Join(parts, " "),
		)

	case *nostr.CloseEnvelope:
		fmt.Printf("%s→ CLOSE %s  %-18s  sub=%s\n",
			clrGray, clrReset,
			ip,
			string(*env),
		)

	case *nostr.AuthEnvelope:
		fmt.Printf("%s→ AUTH  %s  %-18s  id=%s  pub=%s\n",
			clrBMagenta, clrReset,
			ip,
			env.Event.ID.Hex()[:12],
			env.Event.PubKey.Hex()[:12],
		)

	case *nostr.CountEnvelope:
		b, _ := env.Filter.MarshalJSON()
		fmt.Printf("%s→ COUNT %s  %-18s  sub=%-16s  %s\n",
			clrBYellow, clrReset,
			ip,
			wireTrunc(env.SubscriptionID, 16),
			b,
		)

	default:
		fmt.Printf("%s→ ???   %s  %-18s  %s\n",
			clrGray, clrReset,
			ip, wireTrunc(message, 100))
	}
}

func logWSConnect(ip, ua string) {
	uaStr := ""
	if ua != "" {
		uaStr = fmt.Sprintf("  %s%s%s", clrDim, wireTrunc(ua, 60), clrReset)
	}
	fmt.Printf("%s● WS    %s  %-18s  connect%s\n", clrBCyan, clrReset, ip, uaStr)
}

func logWSDisconnect(ip string) {
	fmt.Printf("%s○ WS    %s  %-18s  disconnect\n", clrGray, clrReset, ip)
}

func logHTTPRequest(remoteAddr, method, path string, status int) {
	statusColor := clrGreen
	if status >= 500 {
		statusColor = clrRed
	} else if status >= 400 {
		statusColor = clrYellow
	}
	fmt.Printf("%s  HTTP  %s  %-18s  %-6s  %-40s  %s%d%s\n",
		clrGray, clrReset,
		remoteAddr,
		method,
		wireTrunc(path, 40),
		statusColor, status, clrReset,
	)
}

func logHTTPUpgrade(remoteAddr, path string) {
	fmt.Printf("%s  WS↑   %s  %-18s  %-6s  %s\n",
		clrCyan, clrReset,
		remoteAddr, "GET",
		path,
	)
}
