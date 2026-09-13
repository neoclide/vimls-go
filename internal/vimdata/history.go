package vimdata

import "strings"

// FeatureHistory records the Vim version and commit message when a feature
// was introduced after Vim v9.0.0000.
type FeatureHistory struct {
	Version     string
	Description string
}

// Since returns the hover presentation line, e.g. "Since Vim 9.0.0640".
func (h FeatureHistory) Since() string {
	if h.Version == "" {
		return ""
	}
	return "Since Vim " + h.Version
}

var optionHistory = map[string]FeatureHistory{
	"smoothscroll":         {Version: "9.0.0640", Description: "cannot scroll by screen line if a line wraps"},
	"splitkeep":            {Version: "9.0.0647", Description: "the 'splitscroll' option is not a good name"},
	"lispoptions":          {Version: "9.0.0761", Description: "cannot use 'indentexpr' for Lisp indenting"},
	"endoffile":            {Version: "9.0.0817", Description: "patch 9.0.0817"},
	"keyprotocol":          {Version: "9.0.0930", Description: "cannot debug the Kitty keyboard protocol with TermDebug"},
	"showcmdloc":           {Version: "9.0.1061", Description: "cannot display 'showcmd' somewhere else"},
	"jumpoptions":          {Version: "9.0.1921", Description: "not possible to use the jumplist like a stack"},
	"winfixbuf":            {Version: "9.1.0147", Description: "Cannot keep a buffer focused in a window"},
	"tabclose":             {Version: "9.1.0572", Description: "cannot specify tab page closing behaviour"},
	"completeitemalign":    {Version: "9.1.0754", Description: "fixed order of items in insert-mode completion menu"},
	"findfunc":             {Version: "9.1.0831", Description: "'findexpr' can't be used as lambad or Funcref"},
	"messagesopt":          {Version: "9.1.0908", Description: "not possible to configure :messages"},
	"eventignorewin":       {Version: "9.1.1084", Description: "Unable to persistently ignore events in a window and its buffers"},
	"completefuzzycollect": {Version: "9.1.1178", Description: "not possible to generate completion candidates using fuzzy matching"},
	"pummaxwidth":          {Version: "9.1.1250", Description: "cannot set the maximum popup menu width"},
	"chistory":             {Version: "9.1.1283", Description: "quickfix stack is limited to 10 items"},
	"lhistory":             {Version: "9.1.1283", Description: "quickfix stack is limited to 10 items"},
	"showtabpanel":         {Version: "9.1.1391", Description: "Vim does not have a vertical tabpanel"},
	"tabpanel":             {Version: "9.1.1391", Description: "Vim does not have a vertical tabpanel"},
	"tabpanelopt":          {Version: "9.1.1391", Description: "Vim does not have a vertical tabpanel"},
	"clipmethod":           {Version: "9.1.1485", Description: "missing Wayland clipboard support"},
	"wlseat":               {Version: "9.1.1485", Description: "missing Wayland clipboard support"},
	"wlsteal":              {Version: "9.1.1485", Description: "missing Wayland clipboard support"},
	"wltimeoutlen":         {Version: "9.1.1485", Description: "missing Wayland clipboard support"},
	"maxsearchcount":       {Version: "9.1.1535", Description: "the maximum search count uses hard-coded value 99"},
	"diffanchors":          {Version: "9.1.1557", Description: "not possible to anchor specific lines in difff mode"},
	"autocomplete":         {Version: "9.1.1590", Description: "cannot perform autocompletion"},
	"autocompletedelay":    {Version: "9.1.1638", Description: "completion: not possible to delay the autcompletion"},
	"autocompletetimeout":  {Version: "9.1.1672", Description: "completion: cannot add timeouts for 'cpt' sources"},
	"completetimeout":      {Version: "9.1.1672", Description: "completion: cannot add timeouts for 'cpt' sources"},
	"osctimeoutlen":        {Version: "9.1.1703", Description: "Cannot react to terminal OSC responses"},
	"pumborder":            {Version: "9.1.1835", Description: "completion: not possible to style popup borders globally"},
	"statuslineopt":        {Version: "9.2.0083", Description: "Cannot have a mutli-line statusline"},
	"winhighlight":         {Version: "9.2.0093", Description: "Not possible to have window-local highlighting groups"},
	"termsync":             {Version: "9.2.0110", Description: "No support for terminal synchronization mode"},
	"termresize":           {Version: "9.2.0139", Description: "Cannot configure terminal resize event"},
	"modelinestrict":       {Version: "9.2.0350", Description: "Enabling modelines poses a risk"},
	"scrolloffpad":         {Version: "9.2.0356", Description: "Cannot apply 'scrolloff' context lines at end of file"},
	"tagsecure":            {Version: "9.2.0497", Description: "Cannot jump to remote tags"},
}

var commandHistory = map[string]FeatureHistory{
	"echowindow":     {Version: "9.0.0321", Description: "cannot use the message popup window directly"},
	"horizontal":     {Version: "9.0.0342", Description: "\":wincmd =\" equalizes in two directions"},
	"defer":          {Version: "9.0.0370", Description: "cleaning up afterwards can make a function messy"},
	"pbuffer":        {Version: "9.1.0934", Description: "hard to view an existing buffer in the preview window"},
	"iput":           {Version: "9.1.1213", Description: "cannot :put while keeping indent"},
	"redrawtabpanel": {Version: "9.1.1391", Description: "Vim does not have a vertical tabpanel"},
	"uniq":           {Version: "9.1.1476", Description: "no easy way to deduplicate text"},
	"clipreset":      {Version: "9.1.1485", Description: "missing Wayland clipboard support"},
	"wlrestore":      {Version: "9.1.1485", Description: "missing Wayland clipboard support"},
}

var functionHistory = map[string]FeatureHistory{
	"indexof":                {Version: "9.0.0196", Description: "finding value in list may require a for loop"},
	"getscriptinfo":          {Version: "9.0.0244", Description: "cannot easily get the list of sourced scripts"},
	"setcmdline":             {Version: "9.0.0285", Description: "it is not easy to change the command line from a plugin"},
	"keytrans":               {Version: "9.0.0449", Description: "there is no easy way to translate a key code into a string"},
	"popup_findecho":         {Version: "9.0.0683", Description: "cannot specify a time for :echowindow"},
	"getmouseshape":          {Version: "9.0.0881", Description: "cannot get the currently showing mouse shape"},
	"getbufoneline":          {Version: "9.0.0916", Description: "getbufline() is inefficient for getting a single line"},
	"swapfilelist":           {Version: "9.0.1007", Description: "there is no way to get a list of swap file names"},
	"test_mswin_event":       {Version: "9.0.1084", Description: "code handling low level MS-Windows events cannot be tested"},
	"getcellwidths":          {Version: "9.0.1212", Description: "cannot read back what setcellwidths() has done"},
	"strutf16len":            {Version: "9.0.1485", Description: "no functions for converting from/to UTF-16 index"},
	"utf16idx":               {Version: "9.0.1485", Description: "no functions for converting from/to UTF-16 index"},
	"err_teapot":             {Version: "9.0.1673", Description: "cannot produce a status 418 or 503 message"},
	"instanceof":             {Version: "9.0.1786", Description: "Vim9: need instanceof() function"},
	"matchbufline":           {Version: "9.1.0009", Description: "Cannot easily get the list of matches"},
	"matchstrlist":           {Version: "9.1.0009", Description: "Cannot easily get the list of matches"},
	"foreach":                {Version: "9.1.0027", Description: "Vim is missing a foreach() func"},
	"diff":                   {Version: "9.1.0071", Description: "Need a diff() Vim script function"},
	"getregion":              {Version: "9.1.0120", Description: "hard to get visual region using Vim script"},
	"getregionpos":           {Version: "9.1.0394", Description: "Cannot get a list of positions describing a region"},
	"filecopy":               {Version: "9.1.0465", Description: "missing filecopy() function"},
	"popup_setbuf":           {Version: "9.1.0500", Description: "cannot switch buffer in a popup"},
	"bindtextdomain":         {Version: "9.1.0509", Description: "not possible to translate Vim script messages"},
	"id":                     {Version: "9.1.0548", Description: "it's not possible to get a unique id for some vars"},
	"getcmdprompt":           {Version: "9.1.0741", Description: "No way to get prompt for input()/confirm()"},
	"getcmdcomplpat":         {Version: "9.1.0770", Description: "current command line completion is a bit limited"},
	"getcellpixels":          {Version: "9.1.0854", Description: "cannot get terminal cell size"},
	"base64_decode":          {Version: "9.1.0980", Description: "no support for base64 en-/decoding functions in Vim Script"},
	"base64_encode":          {Version: "9.1.0980", Description: "no support for base64 en-/decoding functions in Vim Script"},
	"getstacktrace":          {Version: "9.1.0984", Description: "exception handling can be improved"},
	"blob2str":               {Version: "9.1.1016", Description: "Not possible to convert string2blob and blob2string"},
	"str2blob":               {Version: "9.1.1016", Description: "Not possible to convert string2blob and blob2string"},
	"ngettext":               {Version: "9.1.1064", Description: "not possible to use plural forms with gettext()"},
	"list2tuple":             {Version: "9.1.1232", Description: "Vim script is missing the tuple data type"},
	"test_null_tuple":        {Version: "9.1.1232", Description: "Vim script is missing the tuple data type"},
	"tuple2list":             {Version: "9.1.1232", Description: "Vim script is missing the tuple data type"},
	"cmdcomplete_info":       {Version: "9.1.1329", Description: "cannot get information about command line completion"},
	"getcompletiontype":      {Version: "9.1.1509", Description: "patch 9.1.1505 was not good"},
	"wildtrigger":            {Version: "9.1.1576", Description: "cannot easily trigger wildcard expansion"},
	"uri_decode":             {Version: "9.1.1669", Description: "Vim script: no support for URI de-/encoding"},
	"uri_encode":             {Version: "9.1.1669", Description: "Vim script: no support for URI de-/encoding"},
	"preinserted":            {Version: "9.1.1797", Description: "completion: autocompletion can be improved"},
	"redraw_listener_add":    {Version: "9.1.1976", Description: "Cannot define callbacks for redraw events"},
	"redraw_listener_remove": {Version: "9.1.1976", Description: "Cannot define callbacks for redraw events"},
	"ch_listen":              {Version: "9.2.0153", Description: "No support to act as a channel server"},
	"tabpanel_getinfo":       {Version: "9.2.0411", Description: "tabpanel: no Vim script functions for the tabpanel"},
	"tabpanel_scroll":        {Version: "9.2.0411", Description: "tabpanel: no Vim script functions for the tabpanel"},
	"getbgcolor":             {Version: "9.2.0612", Description: "Cannot render images in popup windows"},
}

var autocmdEventHistory = map[string]FeatureHistory{
	"TextChangedT":     {Version: "9.0.0756", Description: "no autocmd event for changing text in a terminal window"},
	"WinResized":       {Version: "9.0.0917", Description: "the WinScrolled autocommand event is not enough"},
	"TermResponseAll":  {Version: "9.1.0029", Description: "Cannot act on various terminal response codes"},
	"WinNewPre":        {Version: "9.1.0059", Description: "No event triggered before creating a window"},
	"SessionWritePost": {Version: "9.1.0207", Description: "No autocommand when writing session file"},
	"CursorMovedC":     {Version: "9.1.0507", Description: "hard to detect cursor movement in the command line"},
	"KeyInputPre":      {Version: "9.1.0563", Description: "Cannot process any Key event"},
	"TabClosedPre":     {Version: "9.1.1202", Description: "Missing TabClosedPre autocommand"},
	"CmdlineLeavePre":  {Version: "9.1.1329", Description: "cannot get information about command line completion"},
	"SessionLoadPre":   {Version: "9.2.0061", Description: "Not possible to know when a session will be loaded"},
	"TextPutPost":      {Version: "9.2.0470", Description: "No way to hook into put commands"},
	"TextPutPre":       {Version: "9.2.0470", Description: "No way to hook into put commands"},
}

// LookupOptionHistory looks up the introduction history of a Vim option by name or short name.
func LookupOptionHistory(name string) (FeatureHistory, bool) {
	name = strings.TrimPrefix(name, "&")
	name = strings.TrimPrefix(name, "l:")
	name = strings.TrimPrefix(name, "g:")
	if opt, ok := LookupOption(name); ok {
		name = opt.Name
	}
	item, ok := optionHistory[name]
	return item, ok
}

// LookupCommandHistory looks up the introduction history of an Ex command by name.
func LookupCommandHistory(name string) (FeatureHistory, bool) {
	name = strings.TrimPrefix(name, ":")
	if cmd, ok := Lookup(":" + name); ok {
		name = cmd.Name
	}
	item, ok := commandHistory[name]
	return item, ok
}

// LookupFunctionHistory looks up the introduction history of a built-in function by name.
func LookupFunctionHistory(name string) (FeatureHistory, bool) {
	name = strings.TrimPrefix(name, "g:")
	item, ok := functionHistory[name]
	return item, ok
}

// LookupAutocmdEventHistory looks up the introduction history of an autocmd event by name.
func LookupAutocmdEventHistory(name string) (FeatureHistory, bool) {
	if event, ok := LookupAutocmdEvent(name); ok {
		name = event.Name
	}
	item, ok := autocmdEventHistory[name]
	return item, ok
}
