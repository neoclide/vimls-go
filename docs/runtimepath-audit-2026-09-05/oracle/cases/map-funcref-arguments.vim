vim9script
def Test()
assert_equal(['User1', 'User2'], range(1, 2)->map((_, n: number) => $'User{n}'))
var entries = [{text: 'abc'}]
assert_equal(['abc'], entries->copy()->map((_, entry: dict<any>): string => entry.text))
var pos = [[0, 1]]
var props = pos[0]->copy()->map((_, col: number) => ({col: col + 1, length: 1, type: 'help-fuzzy-toc'}))
assert_equal(2, props[1].col)
var HELP_TEXT = ['one', 'four']
var longest_line: number = HELP_TEXT->copy()->map((_, line: string) => line->strcharlen())->max()
assert_equal(4, longest_line)
var rgb = map(['ff', '00', '10'][: 2], (_, v) => str2nr(v, 16))
assert_equal([255, 0, 16], rgb)
enddef
Test()
