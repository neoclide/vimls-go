" This comment is legacy because Vim has not seen vim9script yet.
vim9script

var script_name: string = 'example'
var values: list<number> = [
  1,
  2,
]

export def Format(value: number): string
  if value > 0
    return $'{script_name}: {value}'
  endif
  return 'empty'
enddef

class Counter
  var value: number

  def Add(amount: number)
    this.value += amount
  enddef
endclass

function Legacy(value)
  let local = a:value
  return local
endfunction
