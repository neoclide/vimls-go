vim9script
setline(1, ['one', 'two', 'three'])
keepjumps :3
assert_equal(3, line('.'))
