package app

import "testing"

func TestAceRecordPriority(t *testing.T) {
	tests := []struct {
		name      string
		mostWins  bool
		candidate aceRecord
		current   aceRecord
		better    bool
	}{
		{name: "多胜先比较胜负差", mostWins: true, candidate: aceRecord{wins: 1, losses: 0}, current: aceRecord{wins: 2, losses: 2}, better: true},
		{name: "多胜相同差值比较胜场", mostWins: true, candidate: aceRecord{wins: 3, losses: 3}, current: aceRecord{wins: 2, losses: 2}, better: true},
		{name: "多败先比较负胜差", mostWins: false, candidate: aceRecord{wins: 0, losses: 1}, current: aceRecord{wins: 2, losses: 2}, better: true},
		{name: "多败相同差值优先较少胜场", mostWins: false, candidate: aceRecord{wins: 2, losses: 2}, current: aceRecord{wins: 3, losses: 3}, better: true},
		{name: "相同战绩进入第二轮抽签", mostWins: true, candidate: aceRecord{wins: 2, losses: 1}, current: aceRecord{wins: 2, losses: 1}, better: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := betterAceRecord(test.candidate, test.current, test.mostWins); got != test.better {
				t.Fatalf("betterAceRecord() = %v, want %v", got, test.better)
			}
		})
	}
}
