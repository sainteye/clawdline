// Command token-ledger reads the transcripts it is given into the token
// ledger's categories and prints them, one line each: a developer's check of
// transcript.LedgerState (docs/token-ledger.md). It reads only the paths on
// its command line and writes nothing.
//
//	go run ./tools/token-ledger <transcript.jsonl> …
package main

import (
	"fmt"
	"os"

	"github.com/sainteye/clawdline/internal/adapters/transcript"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/token-ledger <transcript.jsonl> …")
		os.Exit(2)
	}
	failed := false
	for _, path := range os.Args[1:] {
		if err := show(path); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func show(path string) error {
	var s transcript.LedgerState
	for {
		res, err := s.Feed(path)
		if err != nil {
			return err
		}
		if !res.More {
			break
		}
	}
	spent, measured := s.Totals()
	fmt.Printf("%s\n  %s, model %s: %d calls (%d subagent), %d compactions, peak context %d, %d calls above 200k costing %s\n",
		path, s.Assistant, orNone(s.Model), s.Calls, s.SidechainCalls, s.Compactions, s.PeakContext, s.CallsAbove, cost(s.Above))
	fmt.Printf("  measured %.0f tokens: input %.0f, cache write 1h %.0f / 5m %.0f, cache read %.0f, output %.0f; cost %s\n",
		measured.Total(), measured.Input, measured.CacheWrite1h, measured.CacheWrite5m, measured.CacheRead, measured.Output, cost(sum(spent)))
	if s.Overlong+s.Undecodable > 0 {
		fmt.Printf("  skipped: %d lines past the line limit, %d that did not decode\n", s.Overlong, s.Undecodable)
	}
	total := measured.Total()
	for _, c := range transcript.Categories {
		t := spent[c]
		if t.Total() == 0 {
			continue
		}
		note := ""
		if c == transcript.CategoryRules {
			note = "  (an upper bound)"
		}
		fmt.Printf("  %-10s %5.1f%%  %12.0f tokens  %s%s\n", c, 100*t.Total()/total, t.Total(), cost(t), note)
	}
	c := s.Composition
	if c == nil {
		fmt.Println("  base: composition unknown (no prompt snapshot)")
		return nil
	}
	fmt.Printf("  base %d: system prompt %.0f, skill listing %.0f, MCP instructions %.0f, other %.0f\n",
		c.Measured, c.SystemPrompt, c.SkillListing, c.MCP, c.Other)
	for _, f := range c.Instructions {
		fmt.Printf("    instruction file %-24s %8.0f\n", f.Name, f.Tokens)
	}
	for _, t := range c.Tools {
		fmt.Printf("    tool %-36s %8.0f\n", t.Name, t.Tokens)
	}
	return nil
}

func sum(spent map[transcript.Category]transcript.Tokens) transcript.Tokens {
	var out transcript.Tokens
	for _, c := range transcript.Categories {
		t := spent[c]
		out.Cost += t.Cost
		out.Unpriced += t.Unpriced
	}
	return out
}

func cost(t transcript.Tokens) string {
	if !t.CostKnown() {
		return "unknown"
	}
	return fmt.Sprintf("$%.4f", t.Cost)
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
