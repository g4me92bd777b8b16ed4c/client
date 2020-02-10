package main

import "github.com/faiface/pixel/pixelgl"

type keymap map[keymapping]pixelgl.Button

type keymapping int

const (
	_ keymapping = iota
	KeyToggleChat

	KeyMoveUp
	KeyMoveDown
	KeyMoveLeft
	KeyMoveRight

	KeyCameraUp
	KeyCameraDown
	KeyCameraLeft
	KeyCameraRight

	KeyToggleDebug
	KeyToggleCamLock
	KeyToggleVSync
	KeyToggleFullScreen
	KeyToggleSettings

	KeyAttack

	MouseButtonAttack
	MouseButtonMove

	KeyPageUp
	KeyPageDown

	KeyStartCommand
	KeyStartChat
	KeyToggleMouse
)

func DefaultKeymap() keymap {
	m := make(keymap, len(defaultKeymap))
	for i := range defaultKeymap {
		m[i] = defaultKeymap[i]
	}
	return m
}

var defaultKeymap = keymap{
	KeyToggleChat:    pixelgl.KeyEnter,
	KeyToggleCamLock: pixelgl.KeyEqual,
	KeyToggleVSync:   pixelgl.Key1,

	KeyMoveUp:    pixelgl.KeyW,
	KeyMoveDown:  pixelgl.KeyS,
	KeyMoveLeft:  pixelgl.KeyA,
	KeyMoveRight: pixelgl.KeyD,

	KeyCameraUp:    pixelgl.KeyUp,
	KeyCameraDown:  pixelgl.KeyDown,
	KeyCameraLeft:  pixelgl.KeyLeft,
	KeyCameraRight: pixelgl.KeyRight,

	KeyAttack:   pixelgl.KeySpace,
	KeyPageUp:   pixelgl.KeyPageUp,
	KeyPageDown: pixelgl.KeyPageDown,

	MouseButtonAttack: pixelgl.MouseButtonLeft,
	MouseButtonMove:   pixelgl.MouseButtonRight,

	KeyStartCommand: pixelgl.KeySlash,
	KeyToggleMouse:  pixelgl.Key7,
}
