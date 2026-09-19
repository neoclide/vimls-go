" Curated static import type cases for Vim v9.2.1015.
" Authority: src/testdir/test_vim9_import.vim at the pinned tag.
call writefile([
      \ 'vim9script',
      \ 'export const Count = 1',
      \ "export var Names = ['a']",
      \ 'export final Fixed = true',
      \ ], 'import_values.vim')
call writefile([
      \ 'vim9script',
      \ "import './import_values.vim'",
      \ 'assert_equal(1, import_values.Count)',
      \ "assert_equal('a', import_values.Names[0])",
      \ 'assert_equal(true, import_values.Fixed)',
      \ 'def Bad()',
      \ '  var wrong: string = import_values.Count',
      \ 'enddef',
      \ 'var caught = false',
      \ 'try',
      \ '  defcompile Bad',
      \ 'catch /E1012:/',
      \ '  caught = true',
      \ 'endtry',
      \ 'assert_equal(true, caught)',
      \ ], 'import_consumer.vim')
source import_consumer.vim

call writefile([
      \ 'vim9script',
      \ 'g:import_value_autoload_loaded = true',
      \ 'export var Count = 1',
      \ ], 'autoload_values.vim')
let g:import_value_autoload_loaded = v:false
call writefile([
      \ 'vim9script',
      \ "import autoload './autoload_values.vim' as Lazy",
      \ 'def Read(): number',
      \ '  return Lazy.Count',
      \ 'enddef',
      \ 'defcompile Read',
      \ 'assert_equal(false, g:import_value_autoload_loaded)',
      \ 'assert_equal(1, Read())',
      \ 'assert_equal(true, g:import_value_autoload_loaded)',
      \ ], 'autoload_consumer.vim')
source autoload_consumer.vim
unlet g:import_value_autoload_loaded
