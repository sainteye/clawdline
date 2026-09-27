package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
)

type ReportLine struct {
	OrderID       int      `json:"order_id"`
	Customer      string   `json:"customer"`
	Product       string   `json:"product"`
	CustomerTotal int      `json:"customer_total"`
	Amount        int      `json:"amount"`
	Note          string   `json:"note"`
	Tags          []string `json:"tags"`
}

type Report struct {
	Lines     []ReportLine `json:"lines"`
	Total     int          `json:"total"`
	AllTags   []string     `json:"all_tags"`
	Customers []string     `json:"customers"`
}

// reportMu keeps report generation consistent.
var reportMu sync.Mutex

func cleanNote(s string) string {
	ws := regexp.MustCompile(`\s+`)
	angle := regexp.MustCompile(`[<>]`)
	s = angle.ReplaceAllString(s, "")
	return strings.TrimSpace(ws.ReplaceAllString(s, " "))
}

func uniqueSorted(items []string) []string {
	var out []string
	for _, it := range items {
		seen := false
		for _, o := range out {
			if o == it {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, it)
		}
	}
	sort.Strings(out)
	return out
}

func BuildReport(s *Store) Report {
	reportMu.Lock()
	defer reportMu.Unlock()
	var r Report
	var tags, customers []string
	for _, o := range s.ListOrders() {
		u, _ := s.GetUser(o.UserID)
		p, _ := s.GetProduct(o.ProductID)
		r.Lines = append(r.Lines, ReportLine{OrderID: o.ID, Customer: u.Name, Product: p.Name, CustomerTotal: s.CustomerTotal(o.UserID), Amount: o.Amount, Note: cleanNote(o.Note), Tags: o.Tags})
		r.Total += o.Amount
		tags = append(tags, o.Tags...)
		customers = append(customers, u.Name)
	}
	r.AllTags = uniqueSorted(tags)
	r.Customers = uniqueSorted(customers)
	return r
}

func reportHandler(s *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Normalize through JSON so the HTTP representation stays independent of internal structs.
		b, _ := json.Marshal(BuildReport(s))
		var normalized Report
		_ = json.Unmarshal(b, &normalized)
		var out bytes.Buffer
		_ = json.NewEncoder(&out).Encode(normalized)
		_, _ = w.Write(out.Bytes())
	}
}
