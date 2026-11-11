package planner

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The pairs are from the 2026-11-11 measurement: what Whisper wrote, and what
// the cleanup answered. Every one of them was a cleanup a person would accept.
func TestFaithfulAcceptsTheMeasuredCleanups(t *testing.T) {
	for _, c := range []struct{ raw, polished, level string }{
		{"我剛剛看了一下那個錯誤訊息好像是設定檔,沒有讀到你可以先幫我檢查,路徑對不對如果沒問題的話,再重新跑一次測試。",
			"我剛剛看了一下那個錯誤訊息，好像是設定檔沒有讀到。你可以先幫我檢查路徑對不對，如果沒問題的話，再重新跑一次測試。", PolishPunctuation},
		{"好那就先這樣把我等一下再看。", "好，那就先這樣，我等一下再看。", PolishPunctuation},
		{"幫我把這個branch merge到man,然後跑一下test。如果CI有紅燈的話,先不要deploy。",
			"幫我把這個 branch merge 到 main，然後跑一下 test。如果 CI 有紅燈的話，先不要 deploy。", PolishPunctuation},
		{"你先不要動那個檔案我還在改等我改完了,再跟你講你在處理後面的部分。",
			"你先不要動那個檔案，我還在改，等我改完了再跟你講，你再處理後面的部分。", PolishPunctuation},
		{"恩那個我想問一下就是那個語音,輸入為什麼都沒有標點符號,然後還有每次都要按允許很煩。",
			"我想問一下，語音輸入為什麼都沒有標點符號？還有，每次都要按允許，很煩。", PolishTidy},
		{"呃我覺得這個那個就是commit之後他沒有推上去你幫我看一下是不是那個token過期了",
			"我覺得這個就是commit之後它沒有推上去，你幫我看一下是不是那個token過期了。", PolishTidy},
		{"Can you check why the build failed? I think the config file was not loaded.",
			"Can you check why the build failed? I think the config file was not loaded.", PolishPunctuation},
	} {
		if !Faithful(c.raw, c.polished, c.level) {
			t.Errorf("refused a measured cleanup:\n raw %s\n got %s", c.raw, c.polished)
		}
	}
}

func TestFaithfulRefusesACleanupThatChangedTheWords(t *testing.T) {
	raw := "你先不要動那個檔案我還在改等我改完了,再跟你講你在處理後面的部分。"
	for _, polished := range []string{
		"",           // nothing came back
		"你先不要動那個檔案。", // the second half was dropped
		"好的，我會先處理後面的部分，請稍候。",         // an answer instead of the transcript
		"Don't touch that file yet.", // a translation
	} {
		for _, level := range []string{PolishPunctuation, PolishTidy} {
			if Faithful(raw, polished, level) {
				t.Errorf("%s accepted %q", level, polished)
			}
		}
	}
}

func TestPolishRunsOneToolLessClaudeTurnAndKeepsTheTranscriptInert(t *testing.T) {
	var gotArgs []string
	var gotStdin string
	p := Planner{
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Run: func(_ context.Context, executable string, args []string, stdin, _ string, _ []string) ([]byte, error) {
			if executable != "/bin/claude" {
				t.Fatalf("ran %s", executable)
			}
			gotArgs, gotStdin = args, stdin
			return []byte(`{"structured_output":{"text":"好，那就先這樣，我等一下再看。"}}`), nil
		},
	}
	got, err := p.Polish(context.Background(), "好那就先這樣把我等一下再看。", []string{"Clawdline"}, PolishPunctuation, "Hant", "claude")
	if err != nil || got != "好，那就先這樣，我等一下再看。" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, want := range []string{"--tools", "--strict-mcp-config", "--no-session-persistence", "haiku"} {
		if !slices.Contains(gotArgs, want) {
			t.Errorf("args lack %s: %v", want, gotArgs)
		}
	}
	if i := slices.Index(gotArgs, "--tools"); i < 0 || gotArgs[i+1] != "" {
		t.Errorf("tools are not disabled: %v", gotArgs)
	}
	if !strings.Contains(gotStdin, "<terms>\nClawdline\n</terms>") || !strings.Contains(gotStdin, "<transcript>\n好那就先這樣把我等一下再看。\n</transcript>") {
		t.Errorf("stdin %q", gotStdin)
	}
}

func TestPolishDiscardsAnUnfaithfulAnswer(t *testing.T) {
	p := Planner{
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Run: func(context.Context, string, []string, string, string, []string) ([]byte, error) {
			return []byte(`{"structured_output":{"text":"好的，我知道了。"}}`), nil
		},
	}
	_, err := p.Polish(context.Background(), "你先不要動那個檔案我還在改等我改完了再跟你講", nil, PolishTidy, "Hant", "claude")
	if !errors.Is(err, ErrPolishUnfaithful) {
		t.Fatalf("err %v", err)
	}
}

func TestPolishRefusesWithoutTheChosenAssistant(t *testing.T) {
	p := Planner{LookPath: func(string) (string, error) { return "", errors.New("absent") }, Home: t.TempDir()}
	if _, err := p.Polish(context.Background(), "好", nil, PolishTidy, "", "codex"); !errors.Is(err, ErrNoPlanner) {
		t.Fatalf("err %v", err)
	}
	if _, err := p.Polish(context.Background(), strings.Repeat("字", MaxPolishRunes+1), nil, PolishTidy, "", "claude"); !errors.Is(err, ErrPolishTooLong) {
		t.Fatalf("err %v", err)
	}
}

// The prompt keeps the script the transcript is already in. A Simplified
// speaker's cleanup used to be told to write Traditional Chinese, and then
// either converted silently or refused as unfaithful.
func TestPolishPromptKeepsTheTranscriptsScript(t *testing.T) {
	for script, want := range map[string]string{
		"Hant": "Traditional characters", "Hans": "Simplified characters", "": "do not convert",
		"Latn": "do not convert",
	} {
		got := PolishPrompt(PolishTidy, script)
		if !strings.Contains(got, want) || strings.Contains(got, "%") {
			t.Errorf("%q: %s", script, got)
		}
		for _, other := range []string{"Traditional characters", "Simplified characters"} {
			if other != want && strings.Contains(got, other) {
				t.Errorf("%q also says %s", script, other)
			}
		}
	}
}

func TestPolishThatOutlastsItsBoundIsATimeout(t *testing.T) {
	p := Planner{
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Timeout:  20 * time.Millisecond,
		Run: func(ctx context.Context, _ string, _ []string, _, _ string, _ []string) ([]byte, error) {
			<-ctx.Done()
			return nil, &ExitError{Err: ctx.Err()}
		},
	}
	for _, assistant := range []string{"claude", "codex"} {
		if _, err := p.Polish(context.Background(), "好那就先這樣", nil, PolishTidy, "Hant", assistant); !errors.Is(err, ErrPolishTimeout) {
			t.Errorf("%s: err %v", assistant, err)
		}
	}
}

func TestPolishRunsOneSandboxedCodexTurn(t *testing.T) {
	var gotArgs []string
	var gotStdin string
	p := Planner{
		LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
		Home:     t.TempDir(),
		Run: func(_ context.Context, executable string, args []string, stdin, dir string, _ []string) ([]byte, error) {
			if executable != "/bin/codex" {
				t.Fatalf("ran %s", executable)
			}
			gotArgs, gotStdin = args, stdin
			out := args[slices.Index(args, "-o")+1]
			return nil, os.WriteFile(out, []byte(`{"text":"好，那就先這樣，我等一下再看。"}`), 0o600)
		},
	}
	got, err := p.Polish(context.Background(), "好那就先這樣把我等一下再看。", nil, PolishPunctuation, "Hant", "codex")
	if err != nil || got != "好，那就先這樣，我等一下再看。" {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, pair := range [][2]string{{"-m", polishCodexModel}, {"--sandbox", "read-only"}, {"--disable", "shell_tool"}} {
		i := slices.Index(gotArgs, pair[0])
		if i < 0 || gotArgs[i+1] != pair[1] {
			t.Errorf("args lack %s %s: %v", pair[0], pair[1], gotArgs)
		}
	}
	if !strings.Contains(gotStdin, "Traditional characters") || !strings.Contains(gotStdin, "<transcript>\n好那就先這樣把我等一下再看。\n</transcript>") {
		t.Errorf("stdin %q", gotStdin)
	}
}

// Observed on 2026-11-11 at 18:12 on a phone: Haiku put the whole JSON
// answer inside the text field, and the composer received the braces.
func TestPolishUnwrapsAnAnswerThatWrapsItself(t *testing.T) {
	raw := "來試試看這樣亂講欸不對就是說如果這樣講的話會怎麼樣子"
	want := "來試試看這樣亂講。欸不對。就是說，如果這樣講的話會怎麼樣子？"
	for name, answer := range map[string]string{
		"nested object":  `{"structured_output":{"text":"{\"text\": \"` + want + `\"}"}}`,
		"twice nested":   `{"structured_output":{"text":"{\"text\": \"{\\\"text\\\": \\\"` + want + `\\\"}\"}"}}`,
		"fenced":         `{"structured_output":{"text":"` + "```json\\n{\\\"text\\\": \\\"" + want + "\\\"}\\n```" + `"}}`,
		"result only":    `{"result":"{\"text\": \"` + want + `\"}"}`,
		"result wrapped": `{"result":"{\"text\": \"{\\\"text\\\": \\\"` + want + `\\\"}\"}"}`,
	} {
		p := Planner{
			LookPath: func(name string) (string, error) { return "/bin/" + name, nil },
			Run: func(context.Context, string, []string, string, string, []string) ([]byte, error) {
				return []byte(answer), nil
			},
		}
		got, err := p.Polish(context.Background(), raw, nil, PolishTidy, "Hant", "claude")
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v", name, got, err)
		}
	}
}

// What cannot be unwrapped is refused rather than typed into the composer.
func TestFaithfulRefusesStructureTheDictationNeverHad(t *testing.T) {
	raw := "來試試看這樣亂講欸不對"
	for _, polished := range []string{
		`{"text": "來試試看這樣亂講，欸不對。"`,
		`來試試看這樣亂講，欸不對。"}`,
		`text: 來試試看這樣亂講，欸不對。`,
	} {
		if Faithful(raw, polished, PolishTidy) {
			t.Errorf("accepted %q", polished)
		}
	}
	if !Faithful(`git commit -m "fix {bug}"`, `git commit -m "fix {bug}"。`, PolishTidy) {
		t.Error("braces the speaker said are not structure")
	}
}
