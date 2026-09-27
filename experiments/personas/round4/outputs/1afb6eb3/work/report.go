package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

type ReportLine struct {
	OrderID  int      `json:"order_id"`
	Customer string   `json:"customer"`
	Amount   int      `json:"amount"`
	Note     string   `json:"note"`
	Tags     []string `json:"tags"`
}

type Report struct {
	Lines     []ReportLine `json:"lines"`
	Total     int          `json:"total"`
	AllTags   []string     `json:"all_tags"`
	Customers []string     `json:"customers"`
}

var wsRe = regexp.MustCompile(`\s+`)
var angleRe = regexp.MustCompile(`[<>]`)

func cleanNote(s string) string {
	s = angleRe.ReplaceAllString(s, "")
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

func uniqueSorted(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		if _, ok := seen[it]; !ok {
			seen[it] = struct{}{}
			out = append(out, it)
		}
	}
	sort.Strings(out)
	return out
}

func BuildReport(s *Store) Report {
	var r Report
	orders := s.ListOrders()

	ids := make([]int, 0, len(orders))
	seenIDs := make(map[int]struct{}, len(orders))
	for _, o := range orders {
		if _, ok := seenIDs[o.UserID]; !ok {
			seenIDs[o.UserID] = struct{}{}
			ids = append(ids, o.UserID)
		}
	}
	users := s.GetUsers(ids)

	r.Lines = make([]ReportLine, 0, len(orders))
	tags := make([]string, 0, len(orders)*3)
	customers := make([]string, 0, len(orders))
	for _, o := range orders {
		u := users[o.UserID]
		r.Lines = append(r.Lines, ReportLine{OrderID: o.ID, Customer: u.Name, Amount: o.Amount, Note: cleanNote(o.Note), Tags: o.Tags})
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
		json.NewEncoder(w).Encode(BuildReport(s))
	}
}
