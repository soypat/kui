package kuiglfw

import (
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/soypat/kui"
)

// keys maps GLFW keys to USB HID usages. Unmapped keys are zero.
var keys = makeKeys()

func makeKeys() (t [glfw.KeyLast + 1]kui.Key) {
	for i := range glfw.Key(26) {
		t[glfw.KeyA+i] = kui.KeyA + kui.Key(i)
	}
	for i := range glfw.Key(9) {
		t[glfw.Key1+i] = kui.Key1 + kui.Key(i)
	}
	for i := range glfw.Key(12) {
		t[glfw.KeyF1+i] = kui.KeyF1 + kui.Key(i)
	}
	for _, m := range [...]struct {
		g glfw.Key
		k kui.Key
	}{
		{glfw.Key0, kui.Key0},
		{glfw.KeyEnter, kui.KeyEnter},
		{glfw.KeyKPEnter, kui.KeyEnter},
		{glfw.KeyEscape, kui.KeyEscape},
		{glfw.KeyBackspace, kui.KeyBackspace},
		{glfw.KeyTab, kui.KeyTab},
		{glfw.KeySpace, kui.KeySpace},
		{glfw.KeyMinus, kui.KeyMinus},
		{glfw.KeyEqual, kui.KeyEqual},
		{glfw.KeyLeftBracket, kui.KeyLeftBracket},
		{glfw.KeyRightBracket, kui.KeyRightBracket},
		{glfw.KeyBackslash, kui.KeyBackslash},
		{glfw.KeySemicolon, kui.KeySemicolon},
		{glfw.KeyApostrophe, kui.KeyApostrophe},
		{glfw.KeyGraveAccent, kui.KeyGrave},
		{glfw.KeyComma, kui.KeyComma},
		{glfw.KeyPeriod, kui.KeyPeriod},
		{glfw.KeySlash, kui.KeySlash},
		{glfw.KeyCapsLock, kui.KeyCapsLock},
		{glfw.KeyPrintScreen, kui.KeyPrintScreen},
		{glfw.KeyScrollLock, kui.KeyScrollLock},
		{glfw.KeyPause, kui.KeyPause},
		{glfw.KeyInsert, kui.KeyInsert},
		{glfw.KeyHome, kui.KeyHome},
		{glfw.KeyPageUp, kui.KeyPageUp},
		{glfw.KeyDelete, kui.KeyDelete},
		{glfw.KeyEnd, kui.KeyEnd},
		{glfw.KeyPageDown, kui.KeyPageDown},
		{glfw.KeyRight, kui.KeyArrowRight},
		{glfw.KeyLeft, kui.KeyArrowLeft},
		{glfw.KeyDown, kui.KeyArrowDown},
		{glfw.KeyUp, kui.KeyArrowUp},
		{glfw.KeyLeftControl, kui.KeyLeftCtrl},
		{glfw.KeyLeftShift, kui.KeyLeftShift},
		{glfw.KeyLeftAlt, kui.KeyLeftAlt},
		{glfw.KeyLeftSuper, kui.KeyLeftSuper},
		{glfw.KeyRightControl, kui.KeyRightCtrl},
		{glfw.KeyRightShift, kui.KeyRightShift},
		{glfw.KeyRightAlt, kui.KeyRightAlt},
		{glfw.KeyRightSuper, kui.KeyRightSuper},
	} {
		t[m.g] = m.k
	}
	return t
}

// ToKey returns the USB HID usage of GLFW key k, or zero if kui has no key for it, i.e.
// keypad keys other than Enter, whose text still arrives as text events.
func ToKey(k glfw.Key) kui.Key {
	if k < 0 || k > glfw.KeyLast {
		return 0
	}
	return keys[k]
}

// ToMods returns the kui modifiers of GLFW modifiers m.
func ToMods(m glfw.ModifierKey) kui.KeyMods {
	var mods kui.KeyMods
	if m&glfw.ModShift != 0 {
		mods |= kui.KeyModShift
	}
	if m&glfw.ModControl != 0 {
		mods |= kui.KeyModCtrl
	}
	if m&glfw.ModAlt != 0 {
		mods |= kui.KeyModAlt
	}
	if m&glfw.ModSuper != 0 {
		mods |= kui.KeyModSuper
	}
	if m&glfw.ModCapsLock != 0 {
		mods |= kui.KeyModCapsLock
	}
	if m&glfw.ModNumLock != 0 {
		mods |= kui.KeyModNumLock
	}
	return mods
}

// ToButton returns the kui button of GLFW mouse button b: left, right, middle, back and
// forward. Other buttons return [kui.ButtonNone].
func ToButton(b glfw.MouseButton) kui.PointerButton {
	if b < glfw.MouseButtonLeft || b > glfw.MouseButton5 {
		return kui.ButtonNone
	}
	return kui.ButtonPrimary + kui.PointerButton(b-glfw.MouseButtonLeft)
}
