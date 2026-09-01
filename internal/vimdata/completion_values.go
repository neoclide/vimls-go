package vimdata

import (
	"sort"
	"strings"
)

const (
	CompletionValueVimTag    = "v9.2.1015"
	CompletionValueVimCommit = "5ab969f719bb09555e90e8dff8c94fc37bcbf2ae"
)

type CompletionValue struct {
	Name          string
	Documentation string
}

// sourceHasFeatures mirrors evalfunc.c's has_list[] at CompletionValueVimTag.
var sourceHasFeatures = completionValues(strings.Fields(`amiga
android
arp
haiku
bsd
hpux
hurd
linux
mac
osx
macunix
osxdarwin
qnx
sun
termux
unix
vms
win32
win32unix
win64
ebcdic
fname_case
acl
arabic
autocmd
autochdir
autoservername
socketserver
balloon_eval
balloon_multiline
balloon_eval_term
builtin_terms
all_builtin_terms
browsefilter
byte_offset
channel
cindent
clientserver
clipboard
clipboard_provider
cmdline_compl
cmdline_hist
cmdwin
comments
conceal
cryptv
crypt-blowfish
crypt-blowfish2
cscope
cursorbind
cursorshape
debug
dialog_con
dialog_con_gui
dialog_gui
diff
digraphs
directx
dnd
drop_file
emacs_tags
eval
ex_extra
extra_search
file_in_path
filterpipe
find_in_path
float
folding
footer
fork
gettext
gui
gui_neXtaw
gui_athena
gui_gtk
gui_gtk2
gui_gtk3
gui_gtk4
gui_gnome
gui_haiku
gui_mac
gui_motif
gui_photon
gui_win32
iconv
image
image_cairo
image_gdi
image_gdk
image_kitty
image_sixel
insert_expand
ipv6
job
jumplist
keymap
lambda
langmap
libcall
linebreak
lispindent
listcmds
localmap
lua
menu
mksession
modify_fname
mouse
mouseshape
mouse_dec
mouse_gpm
mouse_jsbterm
mouse_netterm
mouse_pterm
mouse_sgr
mouse_sysmouse
mouse_urxvt
mouse_xterm
multi_byte
multi_byte_ime
multi_lang
mzscheme
nanotime
num64
ole
packages
path_extra
perl
persistent_undo
python_compiled
python_dynamic
python
pythonx
python3_compiled
python3_dynamic
python3_stable
python3
popupwin
postscript
pango
printer
profile
prof_nsec
reltime
quickfix
rightleft
ruby
scrollbind
showcmd
cmdline_info
signs
smartindent
startuptime
statusline
statusline_click
netbeans_intg
sodium
sound
spell
syntax
system
tabpanel
tag_binary
tcl
termguicolors
terminal
terminfo
termresponse
textobjects
textprop
tgetent
timers
title
toolbar
user-commands
user_commands
vartabs
vertsplit
viminfo
vim9script
vimscript-1
vimscript-2
vimscript-3
vimscript-4
virtualedit
visual
visualextra
vreplace
vtp
wayland
wayland_clipboard
wildignore
wildmenu
windows
winaltkeys
writebackup
xattr
xim
xfontset
xpm
xpm_w32
xsmp
xsmp_interact
xterm_clipboard
xterm_save
X11
:tearoff`), "Recognized by has() in Vim v9.2.1015; availability depends on the Vim build and runtime state.")

// dynamicHasFeatures are the fixed spellings handled before has_list[]. The
// patch entry records the pinned ceiling instead of guessing later patches.
var dynamicHasFeatures = []CompletionValue{
	{Name: "browse", Documentation: "Whether the Vim GUI browser is currently available."},
	{Name: "clipboard_working", Documentation: "Whether Vim's clipboard integration is currently available."},
	{Name: "conpty", Documentation: "Whether Vim is using ConPTY on Windows."},
	{Name: "gui_running", Documentation: "Whether the Vim GUI is running or starting."},
	{Name: "mouse_gpm_enabled", Documentation: "Whether GPM mouse support is currently enabled."},
	{Name: "multi_byte_encoding", Documentation: "Whether Vim is currently using a multibyte encoding."},
	{Name: "netbeans_enabled", Documentation: "Whether the NetBeans interface is currently active."},
	{Name: "patch-9.2.1015", Documentation: "Whether Vim includes patch 9.2.1015, the language metadata ceiling used by this server."},
	{Name: "syntax_items", Documentation: "Whether syntax highlighting items exist in the current buffer."},
	{Name: "ttyin", Documentation: "Whether Vim's input is connected to a terminal."},
	{Name: "ttyout", Documentation: "Whether Vim's output is connected to a terminal."},
	{Name: "unnamedplus", Documentation: "Whether the unnamedplus clipboard register is available."},
	{Name: "vcon", Documentation: "Whether Vim is using the Windows virtual console."},
	{Name: "vim_starting", Documentation: "Whether Vim is still starting."},
}

var hasFeatures = func() []CompletionValue {
	values := append(sourceHasFeatures, dynamicHasFeatures...)
	sort.Slice(values, func(left, right int) bool {
		return strings.ToLower(values[left].Name) < strings.ToLower(values[right].Name)
	})
	return values
}()

// expandSpecials follows the finite overview under expand() in builtin.txt.
var expandSpecials = []CompletionValue{
	{Name: "%", Documentation: "Current file name."},
	{Name: "#", Documentation: "Alternate file name; a following number selects alternate file n."},
	{Name: "<cfile>", Documentation: "File name under the cursor."},
	{Name: "<afile>", Documentation: "Autocommand file name."},
	{Name: "<abuf>", Documentation: "Autocommand buffer number as a string."},
	{Name: "<amatch>", Documentation: "Autocommand matched name."},
	{Name: "<cexpr>", Documentation: "C expression under the cursor."},
	{Name: "<sfile>", Documentation: "Sourced script file or function name."},
	{Name: "<slnum>", Documentation: "Sourced script line number or function line number."},
	{Name: "<sflnum>", Documentation: "Script file line number, including inside a function."},
	{Name: "<SID>", Documentation: "Current script ID in <SNR>123_ form."},
	{Name: "<script>", Documentation: "Sourced script file or defining script of the current function."},
	{Name: "<stack>", Documentation: "Current call stack."},
	{Name: "<cword>", Documentation: "Word under the cursor."},
	{Name: "<cWORD>", Documentation: "WORD under the cursor."},
	{Name: "<client>", Documentation: "Client ID of the last received server2client() message."},
}

func completionValues(names []string, documentation string) []CompletionValue {
	values := make([]CompletionValue, len(names))
	for index, name := range names {
		values[index] = CompletionValue{Name: name, Documentation: documentation}
	}
	return values
}

func HasFeatures() []CompletionValue {
	return append([]CompletionValue(nil), hasFeatures...)
}

func ExpandSpecials() []CompletionValue {
	return append([]CompletionValue(nil), expandSpecials...)
}
