package transcript

import "testing"

// The marked payload keeps the call's multiSelect (`m`), and the questions
// read back from it keep it too: a menu made from the transcript alone must
// know a ticking question from one a digit answers.
func TestAskedQuestionsKeepMultiSelect(t *testing.T) {
	text := AskMarker + `[{"q":"下午想喝什麼？","h":"飲料","o":[{"l":"Tea"},{"l":"Water"}]},` +
		`{"q":"要配哪些點心？","h":"點心","m":true,"o":[{"l":"Cookie","d":"脆"},{"l":"Cake"}]}]`
	got, ok := askedQuestions(text)
	if !ok || len(got) != 2 {
		t.Fatalf("read %v %+v", ok, got)
	}
	if got[0].Multi || !got[1].Multi || got[1].Options[0].Note != "脆" {
		t.Fatalf("questions %+v", got)
	}
}
