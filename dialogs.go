package elementary

import (
	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/logger"
)

// maxDialogHistory caps how many dialogs GetDialogs remembers, so a page that shows dialogs in a loop cannot grow
// the history without bound. The oldest entries are dropped first.
const maxDialogHistory = 100

/*
SetDialogOptions is a method which allows you to choose how every JavaScript dialog (alert, confirm, prompt, and
beforeunload) is answered from now on, across every browser context the Instance controls, including contexts
created later. Until this is called, every dialog is dismissed. In addition, the following should be noted:

  - The options are read at the moment each dialog appears, so a change takes effect for the very next dialog,
    including on pages that are already open.

  - The options persist across Restart.

Example:

	agent.SetDialogOptions(elementary.DialogOptions{Accept: true, PromptText: "yes"})
*/
func (shared *Instance) SetDialogOptions(options DialogOptions) {
	shared.dialogMutex.Lock()
	defer shared.dialogMutex.Unlock()
	shared.dialogOptions = options
}

/*
GetDialogs is a method which allows you to list the JavaScript dialogs answered so far, oldest first, along with
their messages and how each was answered. Only the most recent maxDialogHistory dialogs are kept. The returned slice
is a copy, so changing it does not affect the recorded history.

Example:

	for _, dialog := range agent.GetDialogs() {
	    fmt.Println(dialog.Type, dialog.Message)
	}
*/
func (shared *Instance) GetDialogs() []DialogInfo {
	shared.dialogMutex.Lock()
	defer shared.dialogMutex.Unlock()
	return append([]DialogInfo(nil), shared.dialogHistory...)
}

/*
ClearDialogs is a method which allows you to forget every dialog recorded so far, so a later GetDialogs call only
reports dialogs shown after this point. It does not change how dialogs are answered.

Example:

	agent.ClearDialogs()
*/
func (shared *Instance) ClearDialogs() {
	shared.dialogMutex.Lock()
	defer shared.dialogMutex.Unlock()
	shared.dialogHistory = nil
}

/*
handleDialog is a method which allows you to record dialog in the history and answer it according to the current
DialogOptions. It is registered on every browser context by NewContext and runs whenever a page in that context shows
a dialog. In addition, the following should be noted:

  - This method deliberately never takes stateMutex: the page call that triggered the dialog stays blocked until the
    dialog is answered, and that call may already hold stateMutex.

  - The dialog is answered even if recording it fails in some way, since an unanswered dialog freezes its page.

Example:

	context.OnDialog(func(dialog playwright.Dialog) { shared.handleDialog(alias, dialog) })
*/
func (shared *Instance) handleDialog(contextAlias string, dialog playwright.Dialog) {
	shared.dialogMutex.Lock()
	options := shared.dialogOptions
	info := DialogInfo{
		ContextAlias: contextAlias,
		Type:         dialog.Type(),
		Message:      dialog.Message(),
		DefaultValue: dialog.DefaultValue(),
		Accepted:     options.Accept,
	}
	if page := dialog.Page(); page != nil {
		info.PageURL = page.URL()
	}
	shared.dialogHistory = append(shared.dialogHistory, info)
	if overflow := len(shared.dialogHistory) - maxDialogHistory; overflow > 0 {
		shared.dialogHistory = append([]DialogInfo(nil), shared.dialogHistory[overflow:]...)
	}
	shared.dialogMutex.Unlock()

	var err error
	if options.Accept {
		err = dialog.Accept(options.PromptText)
	} else {
		err = dialog.Dismiss()
	}
	if err != nil {
		logger.Log(logger.TYPE_WARN, "failed to answer %s dialog %q: %v", info.Type, info.Message, err)
	}
}
