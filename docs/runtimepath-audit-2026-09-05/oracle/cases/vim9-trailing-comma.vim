vim9script
var b = {toc: {maxlvl: 1}}
var newtitle: string = printf(' %d',
  b.toc.maxlvl,
)
assert_equal(' 1', newtitle)
