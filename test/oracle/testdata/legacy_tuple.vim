" Curated legacy tuple oracle for Vim v9.2.1015.
" Provenance: src/testdir/test_tuple.vim, indexing, slicing, multi-assign,
" tuple iteration and mutable-item cases. No external side effects.
let s:pair = (1, 'two')
let s:alias = s:pair
call assert_equal(1, s:alias[0])
call assert_equal('two', s:pair[-1])
call assert_equal((1,), s:pair[0 : 0])
let [s:count, s:label] = s:pair
let [s:head; s:rest] = s:pair
call assert_equal([1, 'two', 1, ('two',)], [s:count, s:label, s:head, s:rest])
let s:seen = []
for [s:key, s:value] in ((1, 'one'), (2, 'two'))
  call add(s:seen, [s:key, s:value])
endfor
call assert_equal([[1, 'one'], [2, 'two']], s:seen)
let s:mutable = ([1], {'key': 2})
let s:mutable[0][0] = 3
let s:mutable[1].key = 4
call assert_equal(([3], {'key': 4}), s:mutable)
" Legacy tuple += raises E734. Ignoring it leaves the original tuple intact;
" the attempted RHS must not be treated as its new whole value.
let s:extended = ([1],)
silent! let s:extended += ((2,),)
let s:extended[0][0] = 3
call assert_equal(([3],), s:extended)
for [s:command, s:code] in [
      \ ['let s:pair[0] = 3', 'E1532:'],
      \ ['let [s:only] = s:pair', 'E1537:'],
      \ ['let [s:one, s:two, s:three] = s:pair', 'E1538:']]
  try
    execute s:command
    call assert_report('expected ' .. s:code)
  catch
    call assert_match(s:code, v:exception)
  endtry
endfor
