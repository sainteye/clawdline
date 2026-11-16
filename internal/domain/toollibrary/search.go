package toollibrary

import "strings"

// Search combines recommendations with saved sources. A saved source wins when
// both identify the same repository, including differently cased repository IDs.
// Empty queries browse all sources; nonempty queries match any supplied term.
func Search(catalog Catalog, saved []Project, query string) []Project {
	projects := append(append([]Project{}, catalog.Projects...), saved...)
	merged := make([]Project, 0, len(projects))
	positions := map[string]int{}
	for _, p := range projects {
		id := strings.ToLower(p.RepoURL)
		if i, ok := positions[id]; ok {
			merged[i] = p
		} else {
			positions[id] = len(merged)
			merged = append(merged, p)
		}
	}
	terms := strings.Fields(strings.ToLower(query))
	found := []Project{}
	for _, p := range merged {
		words := []string{p.ID, p.Owner, p.Repo, p.RepoURL}
		add := func(names Names) {
			for _, value := range names {
				words = append(words, value)
			}
		}
		add(p.Name)
		add(p.Summary)
		add(p.Category)
		for _, sk := range p.Skills {
			words = append(words, sk.NativeName, sk.Path)
			add(sk.Name)
			add(sk.Purpose)
		}
		text := strings.ToLower(strings.Join(words, "\n"))
		match := len(terms) == 0
		for _, term := range terms {
			match = match || strings.Contains(text, term)
		}
		if match {
			found = append(found, p)
		}
	}
	return found
}
