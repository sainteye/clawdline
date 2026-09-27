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

var (
	wsPattern    = regexp.MustCompile(`\s+`)
	anglePattern = regexp.MustCompile(`[<>]`)
)

func cleanNote(s string) string {
	s = anglePattern.ReplaceAllString(s, "")
	return strings.TrimSpace(wsPattern.ReplaceAllString(s, " "))
}

func uniqueSorted(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	var out []string
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

	userIDs := make([]int, 0, len(orders))
	seenUser := make(map[int]struct{}, len(orders))
	for _, o := range orders {
		if _, ok := seenUser[o.UserID]; !ok {
			seenUser[o.UserID] = struct{}{}
			userIDs = append(userIDs, o.UserID)
		}
	}
	users := s.GetUsers(userIDs)

	var tags, customers []string
	for _, o := range orders {
		name := users[o.UserID].Name
		r.Lines = append(r.Lines, ReportLine{OrderID: o.ID, Customer: name, Amount: o.Amount, Note: cleanNote(o.Note), Tags: o.Tags})
		r.Total += o.Amount
		tags = append(tags, o.Tags...)
		customers = append(customers, name)
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
