package workspace

import "testing"

func TestDecodeStaticPathEscapesAndRejectedValues(t *testing.T) {
	for _, test := range []struct {
		raw, want string
		ok        bool
	}{
		{"'two''quotes'", "two'quotes", true}, {`"\b\e\f\n\r\t"`, "\b\x1b\f\n\r\t", true},
		{`"\101\x42\u0043\U00000044"`, "ABCD", true}, {`"\q\xZ\uZ"`, "qxZuZ", true},
		{"", "", false}, {"$'dynamic'", "", false}, {"'unclosed", "", false}, {"'bad'quote'", "", false},
		{`"\"`, "", false}, {`"\0"`, "", false}, {`"\x00"`, "", false}, {`"\u0000"`, "", false}, {`"\U00110000"`, "", false}, {`"\<Esc>"`, "", false},
	} {
		got, ok := decodeStaticPath(test.raw)
		if got != test.want || ok != test.ok {
			t.Errorf("decodeStaticPath(%q) = %q, %v; want %q, %v", test.raw, got, ok, test.want, test.ok)
		}
	}
	for _, test := range []struct {
		value                    string
		start, limit, want, used int
	}{{"1aZ", 0, 2, 26, 2}, {"", 0, 2, 0, 0}, {"abcd", 0, 2, 0xab, 2}} {
		if got, used := decodeHexEscape(test.value, test.start, test.limit); got != test.want || used != test.used {
			t.Errorf("decodeHexEscape(%q) = %d, %d", test.value, got, used)
		}
	}
	for _, character := range []byte{'0', '9', 'a', 'f', 'A', 'F', 'z'} {
		_ = hexDigit(character)
	}
}
