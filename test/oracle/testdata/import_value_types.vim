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
      \ 'def Bad(): string',
      \ '  return Lazy.Count',
      \ 'enddef',
      \ 'defcompile Read',
      \ 'defcompile Bad',
      \ 'assert_equal(false, g:import_value_autoload_loaded)',
      \ 'assert_equal(1, Read())',
      \ 'assert_equal(true, g:import_value_autoload_loaded)',
      \ 'var caught = false',
      \ 'try',
      \ '  Bad()',
      \ 'catch /E1012:/',
      \ '  caught = true',
      \ 'endtry',
      \ 'assert_true(caught)',
      \ ], 'autoload_consumer.vim')
source autoload_consumer.vim
unlet g:import_value_autoload_loaded

" Re-exports retain inferred types through multiple modules.
let s:bridge =<< trim END
  vim9script
  import './import_values.vim' as values
  export var Count = values.Count + 1
  export var Names = values.Names
  export var Length = (text: string): number => len(text)
END
call writefile(s:bridge, 'import_bridge.vim')
let s:consumer =<< trim END
  vim9script
  import './import_bridge.vim' as bridge
  assert_equal(2, bridge.Count)
  assert_equal('a', bridge.Names[0])
  assert_equal(3, bridge.Length('abc'))
  def Bad()
    var wrong: string = bridge.Count
  enddef
  var caught = false
  try
    defcompile Bad
  catch /E1012:/
    caught = true
  endtry
  assert_true(caught)
END
call writefile(s:consumer, 'transitive_consumer.vim')
source transitive_consumer.vim

" Nominal identity follows definitions, including aliases and base/interface
" relationships. Same-spelled classes in different scripts are distinct.
" Authority: runtime/doc/vim9class.txt and test_vim9_import.vim at v9.2.1015.
let s:model =<< trim END
  vim9script
  export interface Named
    def Label(): string
  endinterface
  export class Item implements Named
    public var id = 1
    def Label(): string
      return 'item'
    enddef
  endclass
  export class Child extends Item
    public var extra = true
  endclass
  export enum State
    Ready,
    Done
  endenum
  export var Value = Child.new()
  export var Values = [Value]
  export var Current = State.Ready
END
call writefile(s:model, 'import_model.vim')
let s:bridge =<< trim END
  vim9script
  import './import_model.vim' as model
  export type Item = model.Item
  export type Named = model.Named
  export type State = model.State
  export var Value = model.Value
  export var Values = model.Values
  export var Current = model.Current
END
call writefile(s:bridge, 'model_bridge.vim')
let s:other =<< trim END
  vim9script
  export class Item
    public var id = 2
  endclass
  export var Value = Item.new()
END
call writefile(s:other, 'other_model.vim')
let s:consumer =<< trim END
  vim9script
  import './import_model.vim' as model
  import './model_bridge.vim' as bridge
  import './other_model.vim' as other
  interface Accepts
    def Get(): model.Item
  endinterface
  class Local extends model.Child implements Accepts
    var held: model.Item = bridge.Value
    def Get(): bridge.Item
      return bridge.Value
    enddef
  endclass
  var base: model.Item = bridge.Value
  var aliased: bridge.Item = model.Value
  var named: bridge.Named = model.Value
  var state: bridge.State = model.Current
  assert_equal('item', named.Label())
  assert_equal(1, base.id)
  assert_equal(1, aliased.id)
  assert_equal(model.State.Ready, state)
  assert_equal(true, bridge.Values[0].extra)
  def Check()
    var aliasObject: model.Item = bridge.Item.new()
    var aliasEnum: model.State = bridge.State.Ready
    var enumName: string = bridge.Current.name
    var enumIndex: number = bridge.Current.ordinal
    var enumValue: bridge.State = model.State.values[0]
    var nothing: model.Item = null_object
    var localBase: bridge.Named = Local.new()
    var [unpacked: model.Item] = bridge.Values
    var Callback = (value: model.Child): model.Item => value
    var called: model.Item = Callback(bridge.Value)
  enddef
  defcompile Check
  def Bad()
    var wrong: model.Item = other.Value
  enddef
  var caught = false
  try
    defcompile Bad
  catch /E1012:/
    caught = true
  endtry
  assert_true(caught)
  def BadLambda()
    var Callback = (): other.Item => bridge.Value
  enddef
  caught = false
  try
    defcompile BadLambda
  catch /E1012:/
    caught = true
  endtry
  assert_true(caught)
END
call writefile(s:consumer, 'nominal_consumer.vim')
source nominal_consumer.vim
