package elementary_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/supercom32/elementary"
)

const formPage = "data:text/html,<html><body>" +
	"<input id='box' type='text' onkeydown=\"if(event.key==='Enter'){this.dataset.entered='yes'}\"/>" +
	"<select id='colour'><option value='r'>Red</option><option value='g'>Green</option></select>" +
	"<select id='many' multiple><option value='a'>A</option><option value='b'>B</option><option value='c'>C</option></select>" +
	"<input id='terms' type='checkbox'/>" +
	"<input id='radio1' type='radio' name='grp' checked/><input id='radio2' type='radio' name='grp'/>" +
	"<div style='height:3000px'></div><footer id='end'>end</footer></body></html>"

var (
	textBoxSelector   = elementary.Selector{Value: "css=#box", Description: "text box"}
	colourSelector    = elementary.Selector{Value: "css=#colour", Description: "colour dropdown"}
	termsSelector     = elementary.Selector{Value: "css=#terms", Description: "terms checkbox"}
	footerSelector    = elementary.Selector{Value: "css=#end", Description: "page footer"}
	firstRadioButton  = elementary.Selector{Value: "css=#radio1", Description: "first radio"}
	secondRadioButton = elementary.Selector{Value: "css=#radio2", Description: "second radio"}
)

/*
TestPressKeyAndPressKeyOnElement is a test which allows you to verify both keyboard methods. PressKeyOnElement types
"x" into the text box, PressKey("Enter") then reaches the focused box and sets data-entered to "yes", and an empty
key returns an error.
*/
func TestPressKeyAndPressKeyOnElement(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, formPage)

	if err := agent.PressKeyOnElement("x", textBoxSelector, nil); err != nil {
		t.Fatalf("PressKeyOnElement failed: %v", err)
	}
	if err := agent.PressKey("Enter"); err != nil {
		t.Fatalf("PressKey failed: %v", err)
	}

	if value := evaluateOrFail(t, agent, "document.querySelector('#box').value"); value != "x" {
		t.Errorf("Expected the text box to contain \"x\", got %v", value)
	}
	if entered := evaluateOrFail(t, agent, "document.querySelector('#box').dataset.entered"); entered != "yes" {
		t.Errorf("Expected Enter to reach the focused text box, got %v", entered)
	}
	if err := agent.PressKey(""); err == nil {
		t.Error("Expected an error for an empty key")
	}
}

/*
TestSelectOptionByValueAndLabel is a test which allows you to verify that SelectOption on a single dropdown accepts a
label ("Green") or a value ("r") and returns the selected value (["g"] then ["r"]), selects ["a", "c"] on a multiple
dropdown, and returns errors for an empty list or a missing dropdown.
*/
func TestSelectOptionByValueAndLabel(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, formPage)

	for input, expected := range map[string][]string{"Green": {"g"}, "r": {"r"}} {
		selected, err := agent.SelectOption(colourSelector, []string{input}, nil)
		if err != nil || !reflect.DeepEqual(selected, expected) {
			t.Errorf("SelectOption(%q): expected %v, got %v (err: %v)", input, expected, selected, err)
		}
	}
	selected, err := agent.SelectOption(elementary.Selector{Value: "css=#many"}, []string{"A", "c"}, nil)
	if err != nil || !reflect.DeepEqual(selected, []string{"a", "c"}) {
		t.Errorf("Expected multiple selection [a c], got %v (err: %v)", selected, err)
	}
	if _, err := agent.SelectOption(colourSelector, nil, nil); err == nil {
		t.Error("Expected an error for an empty value list")
	}
	if _, err := agent.SelectOption(missingElementSelector, []string{"x"}, nil, shortActionOptions); !errors.Is(err, elementary.ErrElementNotFound) {
		t.Errorf("Expected ErrElementNotFound for a missing dropdown, got %v", err)
	}
}

/*
TestSetCheckedAndIsChecked is a test which allows you to verify that the terms checkbox starts unchecked, becomes
checked after SetChecked(true), stays checked after a repeated SetChecked(true), and becomes unchecked after
SetChecked(false). Checking the second radio button unchecks the first, while unchecking a radio button directly
returns an error.
*/
func TestSetCheckedAndIsChecked(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, formPage)
	expectChecked := func(selector elementary.Selector, expected bool) {
		t.Helper()
		if checked, err := agent.IsChecked(selector, nil); err != nil || checked != expected {
			t.Errorf("Expected %s checked=%t, got %t (err: %v)", selector, expected, checked, err)
		}
	}

	expectChecked(termsSelector, false)
	for _, state := range []bool{true, true, false} {
		if err := agent.SetChecked(termsSelector, state, nil); err != nil {
			t.Fatalf("SetChecked(%t) failed: %v", state, err)
		}
		expectChecked(termsSelector, state)
	}

	if err := agent.SetChecked(secondRadioButton, true, nil); err != nil {
		t.Fatalf("SetChecked on the second radio failed: %v", err)
	}
	expectChecked(firstRadioButton, false)
	expectChecked(secondRadioButton, true)
	if err := agent.SetChecked(secondRadioButton, false, nil, shortActionOptions); err == nil {
		t.Error("Expected an error when unchecking a radio button directly")
	}
}

/*
TestScrollToElementAndScrollPageBy is a test which allows you to verify scrolling on a 3000px-tall page. ScrollPageBy(0,
500) leaves window.scrollY at 500, and ScrollToElement on the footer below the tall block scrolls past 2000px.
*/
func TestScrollToElementAndScrollPageBy(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, formPage)

	if err := agent.ScrollPageBy(0, 500); err != nil {
		t.Fatalf("ScrollPageBy failed: %v", err)
	}
	if scrollY := evaluateOrFail(t, agent, "Math.round(window.scrollY)"); scrollY != 500 {
		t.Errorf("Expected scrollY 500 after ScrollPageBy, got %v", scrollY)
	}

	if err := agent.ScrollToElement(footerSelector, nil); err != nil {
		t.Fatalf("ScrollToElement failed: %v", err)
	}
	scrollY, isInt := evaluateOrFail(t, agent, "Math.round(window.scrollY)").(int)
	if !isInt || scrollY < 2000 {
		t.Errorf("Expected ScrollToElement to scroll past 2000px, got %v", scrollY)
	}
}
