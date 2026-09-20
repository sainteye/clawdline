package snippet

import (
	"strings"
	"testing"
)

func TestAUnknownFieldIsRefusedRatherThanIgnored(t *testing.T) {
	_, ref := ParseFields(map[string]any{"title": "t", "body": "b", "scope": "global", "postition": "1"})
	if ref == nil || ref.Code != CodeMalformed {
		t.Fatalf("an unknown key was accepted: %v", ref)
	}
}

func TestAFieldThatIsNotTextIsRefused(t *testing.T) {
	_, ref := ParseFields(map[string]any{"title": 7})
	if ref == nil || ref.Code != CodeMalformed {
		t.Fatalf("a numeric title was accepted: %v", ref)
	}
}

// The bound is taken on the value as it arrived, and a normaliser that
// truncates cannot make an over-long path fit. This is the whole reason
// ParseFields measures and Normalized does not.
func TestAnOverlongProjectPathIsRefusedBeforeItIsNormalised(t *testing.T) {
	long := "/" + strings.Repeat("p", MaxProjectBytes)
	fields, ref := ParseFields(map[string]any{"title": "t", "body": "b", "scope": "project", "project": long})
	if ref == nil || ref.Code != CodeTooLong {
		t.Fatalf("a %d-byte path was accepted: %v", len(long), ref)
	}
	if got := ref.Extra["project_count"]; got != int64(len(long)) {
		t.Errorf("the refusal counted %d bytes, not %d", got, len(long))
	}
	// And the truncating normaliser would have let it through, which is what
	// makes the order of the two load-bearing.
	cut := fields.Normalized(func(p string) string { return p[:MaxProjectBytes] })
	if len(cut.Project) > MaxProjectBytes {
		t.Fatal("the fixture does not truncate; the test is not testing anything")
	}
}

func TestATitleIsMeasuredInBytesAndNotInCharacters(t *testing.T) {
	// Sixty-seven Han characters: 67 by String.count, 201 as UTF-8.
	han := strings.Repeat("句", 67)
	if len(han) != 201 {
		t.Fatalf("the fixture is %d bytes", len(han))
	}
	_, ref := ParseFields(map[string]any{"title": han, "body": "b", "scope": "global"})
	if ref == nil || ref.Code != CodeTooLong {
		t.Fatalf("201 bytes of title were accepted: %v", ref)
	}
	_, ref = ParseFields(map[string]any{"title": strings.Repeat("句", 66), "body": "b", "scope": "global"})
	if ref != nil {
		t.Fatalf("198 bytes of title were refused: %v", ref)
	}
}

func TestACreateNeedsItsThreeFields(t *testing.T) {
	for _, body := range []map[string]any{
		{"body": "b", "scope": "global"},
		{"title": "t", "scope": "global"},
		{"title": "t", "body": "b"},
	} {
		fields, ref := ParseFields(body)
		if ref != nil {
			t.Fatal(ref)
		}
		if _, ref := New("id", fields, 0, 1); ref == nil || ref.Code != CodeMalformed {
			t.Errorf("%v was accepted: %v", body, ref)
		}
	}
}

func TestAScopeAndItsProjectAgreeInBothDirections(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		code string
	}{
		{"a global snippet carrying a project", map[string]any{
			"title": "t", "body": "b", "scope": "global", "project": "/p"}, CodeScopeMismatch},
		{"a global snippet carrying an empty project", map[string]any{
			"title": "t", "body": "b", "scope": "global", "project": ""}, CodeScopeMismatch},
		{"a project snippet naming none", map[string]any{
			"title": "t", "body": "b", "scope": "project"}, CodeScopeMismatch},
		{"a project snippet naming an empty one", map[string]any{
			"title": "t", "body": "b", "scope": "project", "project": ""}, CodeScopeMismatch},
		{"a scope that is neither", map[string]any{
			"title": "t", "body": "b", "scope": "everywhere"}, CodeMalformed},
	}
	for _, c := range cases {
		fields, ref := ParseFields(c.body)
		if ref != nil {
			t.Fatalf("%s: %v", c.name, ref)
		}
		_, ref = New("id", fields, 0, 1)
		if ref == nil || ref.Code != c.code {
			t.Errorf("%s: %v, want %s", c.name, ref, c.code)
		}
	}
}

func TestAnEmptyTitleOrBodyIsRefused(t *testing.T) {
	for _, body := range []map[string]any{
		{"title": "  ", "body": "b", "scope": "global"},
		{"title": "t", "body": "\n\t ", "scope": "global"},
	} {
		fields, _ := ParseFields(body)
		if _, ref := New("id", fields, 0, 1); ref == nil || ref.Code != CodeMalformed {
			t.Errorf("%v was accepted: %v", body, ref)
		}
	}
}

// A snippet is literal text: the newlines and the leading spaces inside it are
// what the person typed, and only the ends of the title are tidied.
func TestABodysOwnWhitespaceIsKept(t *testing.T) {
	fields, _ := ParseFields(map[string]any{
		"title": "  t  ", "body": "  one\n    two\n", "scope": "global"})
	made, ref := New("id", fields, 100, 7)
	if ref != nil {
		t.Fatal(ref)
	}
	if made.Title != "t" {
		t.Errorf("title %q", made.Title)
	}
	if made.Body != "  one\n    two\n" {
		t.Errorf("body %q", made.Body)
	}
}

func TestASaveKeepsWhatItDoesNotName(t *testing.T) {
	was := Record{ID: "a", Title: "old", Body: "text", Scope: Project, Project: "/p",
		Position: 300, CreatedAt: 11, UpdatedAt: 11}
	fields, ref := ParseFields(map[string]any{"title": "new"})
	if ref != nil {
		t.Fatal(ref)
	}
	next, ref := Patch(was, fields, 99)
	if ref != nil {
		t.Fatal(ref)
	}
	if next.Title != "new" || next.Body != "text" || next.Scope != Project || next.Project != "/p" {
		t.Errorf("%+v", next)
	}
	if next.CreatedAt != 11 || next.UpdatedAt != 99 || next.Position != 300 {
		t.Errorf("times or place moved: %+v", next)
	}
	if Moved(was, next) {
		t.Error("a title change moved the snippet to another group")
	}
}

func TestASaveWithNoFieldsIsRefused(t *testing.T) {
	fields, _ := ParseFields(map[string]any{})
	if _, ref := Patch(Record{ID: "a", Title: "t", Body: "b", Scope: Global}, fields, 1); ref == nil ||
		ref.Code != CodeMalformed {
		t.Fatalf("an empty save was accepted: %v", ref)
	}
}

func TestMovingToEveryProjectDropsTheProjectPath(t *testing.T) {
	was := Record{ID: "a", Title: "t", Body: "b", Scope: Project, Project: "/p", Position: 100}
	fields, _ := ParseFields(map[string]any{"scope": "global"})
	next, ref := Patch(was, fields, 5)
	if ref != nil {
		t.Fatal(ref)
	}
	if next.Scope != Global || next.Project != "" {
		t.Fatalf("%+v", next)
	}
	if !Moved(was, next) {
		t.Error("a scope change is a move and has to be given a new place")
	}
}

func TestOneGroupsOrderIsCompleteOrItIsRefused(t *testing.T) {
	group := []Record{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	for _, ids := range [][]string{
		{"a", "b"},
		{"a", "b", "c", "d"},
		{"a", "b", "z"},
		{"a", "b", "b"},
	} {
		if _, ref := Order(group, ids); ref == nil || ref.Code != CodeScopeMismatch {
			t.Errorf("%v was accepted: %v", ids, ref)
		}
	}
	moved, ref := Order(group, []string{"c", "a", "b"})
	if ref != nil {
		t.Fatal(ref)

	}
	if len(moved) != 3 || moved[0].ID != "c" || moved[0].Position != 100 ||
		moved[1].Position != 200 || moved[2].Position != 300 {
		t.Errorf("%+v", moved)
	}
}

func TestTheOrderIsThePersonsAndThenArrivals(t *testing.T) {
	rows := []Record{
		{ID: "c", Position: 100, CreatedAt: 9},
		{ID: "a", Position: 100, CreatedAt: 3},
		{ID: "b", Position: 50, CreatedAt: 20},
	}
	Sort(rows)
	if rows[0].ID != "b" || rows[1].ID != "a" || rows[2].ID != "c" {
		t.Errorf("%v %v %v", rows[0].ID, rows[1].ID, rows[2].ID)
	}
}

func TestASessionSeesItsOwnProjectAndThenEveryProject(t *testing.T) {
	rows := []Record{
		{ID: "g1", Scope: Global, Position: 200},
		{ID: "p1", Scope: Project, Project: "/here", Position: 200},
		{ID: "other", Scope: Project, Project: "/elsewhere", Position: 100},
		{ID: "g2", Scope: Global, Position: 100},
		{ID: "p2", Scope: Project, Project: "/here", Position: 100},
	}
	got := []string{}
	for _, r := range InScope(rows, "/here") {
		got = append(got, r.ID)
	}
	if strings.Join(got, ",") != "p2,p1,g2,g1" {
		t.Errorf("%v", got)
	}
	// A session whose project could not be resolved sees the global group and
	// nothing else — a smaller answer, never somebody else's snippets.
	got = got[:0]
	for _, r := range InScope(rows, "") {
		got = append(got, r.ID)
	}
	if strings.Join(got, ",") != "g2,g1" {
		t.Errorf("%v", got)
	}
}

func TestBothLimitsRefuseAndNeitherEvicts(t *testing.T) {
	limits := Limits{Total: 100, Scope: 50}
	if ref := LimitRefusal(99, 49, limits); ref != nil {
		t.Fatalf("room was refused: %v", ref)
	}
	ref := LimitRefusal(100, 0, limits)
	if ref == nil || ref.Code != CodeLimitReached || ref.Status != 409 ||
		ref.Extra["limit"] != 100 || ref.Extra["count"] != 100 {
		t.Fatalf("the machine's row: %v", ref)
	}
	ref = LimitRefusal(60, 50, limits)
	if ref == nil || ref.Extra["limit"] != 50 {
		t.Fatalf("the group's row: %v", ref)
	}
}

func TestANewMemberGoesAfterTheLastAndNeverPastTheCeiling(t *testing.T) {
	if got := NextPosition(0); got != 100 {
		t.Errorf("first: %d", got)
	}
	if got := NextPosition(300); got != 400 {
		t.Errorf("next: %d", got)
	}
	if got := NextPosition(MaxPosition); got != MaxPosition {
		t.Errorf("ceiling: %d", got)
	}
}
