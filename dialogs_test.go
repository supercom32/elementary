package elementary_test

import (
	"testing"

	"github.com/supercom32/elementary"
)

/*
TestDialogsAreDismissedByDefaultAndRecorded is a test which allows you to verify the default dialog handling. An
alert never blocks the script ("alert('hi'); 'done'" returns "done"), confirm('Delete?') returns false, and
GetDialogs then reports two entries, the second being {Type: "confirm", Message: "Delete?", Accepted: false}.
ClearDialogs empties the history.
*/
func TestDialogsAreDismissedByDefaultAndRecorded(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body>dialogs</body></html>")

	if result := evaluateOrFail(t, agent, "alert('hi'); 'done'"); result != "done" {
		t.Errorf("Expected the alert not to block the script, got %v", result)
	}
	if result := evaluateOrFail(t, agent, "confirm('Delete?')"); result != false {
		t.Errorf("Expected confirm to be dismissed by default, got %v", result)
	}

	dialogs := agent.GetDialogs()
	if len(dialogs) != 2 {
		t.Fatalf("Expected 2 recorded dialogs, got %d: %+v", len(dialogs), dialogs)
	}
	confirmDialog := dialogs[1]
	if confirmDialog.Type != "confirm" || confirmDialog.Message != "Delete?" || confirmDialog.Accepted || confirmDialog.ContextAlias != "main" {
		t.Errorf("Unexpected confirm record: %+v", confirmDialog)
	}
	agent.ClearDialogs()
	if remaining := agent.GetDialogs(); len(remaining) != 0 {
		t.Errorf("Expected ClearDialogs to empty the history, got %+v", remaining)
	}
}

/*
TestDialogOptionsAcceptAndAnswerPrompts is a test which allows you to verify that after
SetDialogOptions(DialogOptions{Accept: true, PromptText: "Ada"}), confirm('Sure?') returns true and
prompt('Name?', 'nobody') returns "Ada", recorded with DefaultValue "nobody" and Accepted true.
*/
func TestDialogOptionsAcceptAndAnswerPrompts(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body>dialogs</body></html>")
	agent.SetDialogOptions(elementary.DialogOptions{Accept: true, PromptText: "Ada"})

	if result := evaluateOrFail(t, agent, "confirm('Sure?')"); result != true {
		t.Errorf("Expected confirm to be accepted, got %v", result)
	}
	if result := evaluateOrFail(t, agent, "prompt('Name?', 'nobody')"); result != "Ada" {
		t.Errorf("Expected the prompt to be answered with \"Ada\", got %v", result)
	}

	dialogs := agent.GetDialogs()
	promptDialog := dialogs[len(dialogs)-1]
	if promptDialog.Type != "prompt" || promptDialog.DefaultValue != "nobody" || !promptDialog.Accepted {
		t.Errorf("Unexpected prompt record: %+v", promptDialog)
	}
}
