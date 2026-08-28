package narrative

import "testing"

func TestDisplayParagraphsKeepsExplicitParagraphs(t *testing.T) {
	in := "one\n\ntwo\n\nthree\n\nfour\n\nfive"
	got := DisplayParagraphs(in)
	if len(got) != 5 || got[2] != "three" {
		t.Fatalf("unexpected paragraphs: %#v", got)
	}
}

func TestDisplayParagraphsSplitsLongWallOfText(t *testing.T) {
	in := "One. Two. Three. Four. Five. Six. Seven. Eight. Nine. Ten. Eleven. Twelve. Thirteen. Fourteen. Fifteen. Sixteen. Seventeen. Eighteen. Nineteen. Twenty. Twenty one. Twenty two. Twenty three. Twenty four. Twenty five. Twenty six. Twenty seven. Twenty eight. Twenty nine. Thirty. Thirty one. Thirty two. Thirty three. Thirty four. Thirty five. Thirty six. Thirty seven. Thirty eight. Thirty nine. Forty."
	got := DisplayParagraphs(in)
	if len(got) < 7 || len(got) > 9 {
		t.Fatalf("expected 7-9 display paragraphs, got %d", len(got))
	}
}
