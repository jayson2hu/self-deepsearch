package searchtext

import "testing"

func TestCharacterEquivalence(t *testing.T) {
	for _, pair := range [][2]string{{"橋本", "桥本"}, {"三上悠亜", "三上悠亚"}, {"三上悠亞", "三上悠亚"}, {"ＡＩＫＡ", "aika"}, {"ｱｲｶ", "アイカ"}, {"詠", "咏"}} {
		if Fold(pair[0]) != Fold(pair[1]) {
			t.Errorf("not equivalent: %v", pair)
		}
	}
	for _, value := range []string{"橋本", "三上悠亜", "ＡＩＫＡ", "ｱｲｶ"} {
		if Fold(Fold(value)) != Fold(value) {
			t.Errorf("not idempotent: %s", value)
		}
	}
	if Fold("深田えいみ") == Fold("深田咏美") {
		t.Fatal("must not invent translated names")
	}
}

func TestQueryScopedTable(t *testing.T){
 from,to:=TableFor("桥本")
 if len([]rune(from))!=len([]rune(to))||len([]rune(from))>10{t.Fatal("unbounded per-query variant table")}
 found:=false;for i,c:=range []rune(from){if c=='橋'&&[]rune(to)[i]=='桥'{found=true}}
 if !found{t.Fatal("required variant missing")}
 from,to=TableFor("AIKA");if from!=""||to!=""{t.Fatal("Latin query does not need Han variants")}
}
