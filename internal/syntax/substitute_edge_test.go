package syntax

import "testing"

func TestSubstituteScannerPrimitiveBoundaries(t *testing.T) {
	for _, test := range []struct {
		byte byte
		bit  SubstituteFlags
		ok   bool
	}{
		{'g', SubstituteFlagAll, true}, {'c', SubstituteFlagConfirm, true}, {'n', SubstituteFlagCount, true}, {'e', SubstituteFlagError, true}, {'r', SubstituteFlagLastPattern, true}, {'p', SubstituteFlagPrint, true}, {'#', SubstituteFlagNumber, true}, {'l', SubstituteFlagList, true}, {'i', SubstituteFlagIgnoreCase, true}, {'I', SubstituteFlagMatchCase, true}, {'x', 0, false},
	} {
		if bit, ok := substituteFlagBit(test.byte); bit != test.bit || ok != test.ok {
			t.Errorf("flag %q = %v,%v", test.byte, bit, ok)
		}
	}
	for _, test := range []struct{ end, limit, want int }{{0, 1, 0}, {1, 1, 1}, {2, 1, 1}, {-1, 1, -1}} {
		if got := minSpanEnd(test.end, test.limit); got != test.want {
			t.Errorf("min(%d,%d)=%d", test.end, test.limit, got)
		}
	}
	if CommandInsideFunction(nil, nil) {
		t.Fatal("nil command is inside a function")
	}
	command := &Command{Block: 1}
	blocks := []Block{{Kind: BlockIf, Parent: -1}, {Kind: BlockDef, Parent: 0}}
	if !CommandInsideFunction(command, blocks) {
		t.Fatal("def block not detected")
	}
	blocks[1].Kind = BlockFunction
	if !CommandInsideFunction(command, blocks) {
		t.Fatal("function block not detected")
	}
	blocks[1].Kind = BlockIf
	if CommandInsideFunction(command, blocks) {
		t.Fatal("ordinary block detected as function")
	}
	command.Block = -2
	if CommandInsideFunction(command, blocks) {
		t.Fatal("invalid block accepted")
	}
}
