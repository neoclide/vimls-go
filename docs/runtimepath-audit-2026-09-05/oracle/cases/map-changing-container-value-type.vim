vim9script
const patterns = {class: ['export', 'abstract']}
  ->map((_, mods: list<string>): string =>
    '\%(' .. mods
    ->join('\|')
    .. '\)')
assert_equal('\%(export\|abstract\)', patterns.class)
const BLOCKS: list<list<string>> = [['if', 'endif'], ['for', 'endfor']]
const ENDS_BLOCK: string = '^'
  .. BLOCKS
  ->copy()
  ->map((_, kwds: list<string>): string => kwds[-1])
  ->join('\|')
assert_equal('^endif\|endfor', ENDS_BLOCK)
