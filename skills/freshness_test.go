package skills

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
)

func pinFor(source, translation []byte) guidePin {
	return guidePin{
		Source:      fmt.Sprintf("%x", sha256.Sum256(source)),
		Translation: fmt.Sprintf("%x", sha256.Sum256(translation)),
	}
}

func TestStaleGuidePartFallsBackToEnglish(t *testing.T) {
	english, err := files.ReadFile(topics["en"])
	if err != nil {
		t.Fatal(err)
	}
	translated, err := files.ReadFile(topics["zh-Hant"])
	if err != nil {
		t.Fatal(err)
	}
	ep, es := split(english)
	tp, ts := split(translated)
	if len(es) != len(sections) || len(ts) != len(sections) {
		t.Fatal("test guides do not match the section table")
	}
	pins := map[string]guidePin{"intro": pinFor(ep, tp)}
	for i, section := range sections {
		pins[section.Name] = pinFor(es[i], ts[i])
	}
	fresh, pending, err := mergeGuide(english, translated, "zh-Hant", pins)
	if err != nil || len(pending) != 0 || !bytes.Equal(fresh, translated) {
		t.Fatalf("fresh translation: pending=%v err=%v", pending, err)
	}

	changed := bytes.Replace(english, []byte("[--timeout 90]"), []byte("[--timeout 120]"), 1)
	got, pending, err := mergeGuide(changed, translated, "zh-Hant", pins)
	if err != nil || !reflect.DeepEqual(pending, []string{"dispatch"}) {
		t.Fatalf("changed source: pending=%v err=%v", pending, err)
	}
	_, parts := split(got)
	if !bytes.Contains(parts[7], []byte("[--timeout 120]")) {
		t.Fatal("stale dispatch command was not replaced by revised English")
	}
	if !bytes.Equal(parts[8], ts[8]) {
		t.Fatal("a current neighboring translation was replaced")
	}
	if !bytes.Contains(got, []byte("dispatch")) || !bytes.Contains(got, []byte(pendingNotice["zh-Hant"])) {
		t.Fatal("pending part was not listed in the guide")
	}

	damaged := bytes.Replace(translated, []byte("## 4. 派出一個 owned child"), []byte("## 4. 受損的譯文"), 1)
	_, pending, err = mergeGuide(english, damaged, "zh-Hant", pins)
	if err != nil || !reflect.DeepEqual(pending, []string{"dispatch"}) {
		t.Fatalf("changed translation: pending=%v err=%v", pending, err)
	}
}

func TestTraditionalChineseGuideIsCurrent(t *testing.T) {
	pending, err := PendingSections("zh-Hant")
	if err != nil || len(pending) != 0 {
		t.Fatalf("zh-Hant translation must track English immediately: pending=%v err=%v", pending, err)
	}
}
