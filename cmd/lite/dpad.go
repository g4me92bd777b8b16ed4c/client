package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/pixelgl"
	"github.com/g4me92bd777b8b16ed4c/common"
	"github.com/g4me92bd777b8b16ed4c/common/codec"
	"github.com/g4me92bd777b8b16ed4c/common/types"
	"github.com/g4me92bd777b8b16ed4c/common/updater"
)

const playerSpeed = 8

func (g *Game) controldpad(dt float64) (continueNext bool, err error) {

	var (
		dir  = pixel.ZV
		dpad = byte(0)
		pos  = pixel.V(g.me.X(), g.me.Y())
	)

	inputbuf := g.inputbuf
	if g.win.JustPressed(pixelgl.KeyEscape) || (g.win.JustPressed(pixelgl.KeyQ) && g.win.Pressed(pixelgl.KeyLeftControl)) {
		return false, errors.New("quit")
	}

	if g.settings.typing && g.win.JustPressed(pixelgl.KeyBackspace) {
		if g.inputbuf.Len() != 0 {
			g.inputbuf.Truncate(g.inputbuf.Len() - 1)
		}
	}
	if g.settings.typing {
		fmt.Fprintf(&g.inputbuf, "%s", g.win.Typed())
		//	log.Println("Typing:", inputbuf.String())
	}
	if !g.settings.typing && g.win.JustPressed(g.settings.keymap[KeyStartCommand]) {
		g.settings.typing = true
		fmt.Fprintf(&g.inputbuf, "%s", "/")
	}
	if g.win.JustPressed(pixelgl.KeyEnter) {
		g.settings.typing = !g.settings.typing
		if !g.settings.typing && g.inputbuf.Len() != 0 {
			if strings.HasPrefix(g.inputbuf.String(), "/") {
				g.handleChatConsole()
				g.inputbuf.Reset()
			} else {
				log.Println("Sending typed:", g.inputbuf.String())
				n, err := g.codec.Write(common.PlayerMessage{From: g.playerid, To: 0, Message: basex.Encode(g.chatcrypt.Encrypt(inputbuf.Bytes()))})
				if err != nil {
					log.Fatalln(err)
				}
				g.stats.netsent += n
				g.inputbuf.Reset()
			}

		}
	}
	if !g.settings.typing && g.inputbuf.Len() != 0 {
		g.inputbuf.Reset()
	}

	if !g.settings.typing {
		if g.win.Pressed(g.settings.keymap[KeyMoveUp]) {
			dir.Y += 1
			dpad |= (common.UP)
		}
		if g.win.Pressed(g.settings.keymap[KeyMoveDown]) {
			dir.Y -= 1
			dpad |= (common.DOWN)
		}
		if g.win.Pressed(g.settings.keymap[KeyMoveLeft]) {
			dir.X -= 1
			dpad |= (common.LEFT)
		}
		if g.win.Pressed(g.settings.keymap[KeyMoveRight]) {
			dir.X += 1
			dpad |= (common.RIGHT)
		}
		angle2dpad := func(v pixel.Vec) byte {
			v.X = math.Floor(v.X)
			v.Y = math.Floor(v.Y)
			//log.Println("Angle2dpad:", v)
			switch v {
			case pixel.V(0, 1):
				return common.UP
			case pixel.V(1, 1):
				return common.UPRIGHT
			case pixel.V(-1, 1):
				return common.UPLEFT
			case pixel.V(0, -1):
				return common.DOWN
			case pixel.V(1, -1):
				return common.DOWNRIGHT
			case pixel.V(-1, 0):
				return common.LEFT
			case pixel.V(1, 0):
				return common.RIGHT
			case pixel.V(-1, -1):
				return common.DOWNLEFT
			default:
				log.Println("UNKONWN ANGLE:", v)
				log.Println(dpad)
				return 0
			}
		}
		if dpad == 0 {
			if g.win.Pressed(g.settings.keymap[MouseButtonMove]) {
				dpad = angle2dpad(g.win.MousePosition().Sub(g.win.Bounds().Center()).Unit().Add(pixel.V(0.4, 0.4)))
				//log.Println("MOUSE DIR:", common.DPAD(dpad))
			}
		}

		// if g.win.JustPressed(g.settings.keymap[KeyToggleMouse]) {
		// 	if g.settings.mousedisabled {
		// 		g.win.SetCursorDisabled()
		// 	} else {
		// 		g.win.SetCursorVisible(true)
		// 	}
		// 	g.settings.mousedisabled = !g.settings.mousedisabled
		// }
		// if dpad == 0 || fps % 10 == 0 {
		// 	//xpos := g.world.Get(g.playerid)
		// 	//pos = pixel.Lerp(pos, pixel.V(xpos.X(), xpos.Y()), 0.5)
		// }
		action := common.PlayerAction{}
		if g.win.JustPressed(g.settings.keymap[KeyAttack]) || g.win.JustPressed(g.settings.keymap[MouseButtonAttack]) {
			action.Action = types.ActionManastorm.Uint16()
			g.animations.Push(types.ActionManastorm, pos)
			//g.flashMessage("Manastorm!")
		}

		// run all plugin update fns
		for _, updatefn := range g.updateFns {
			updatefn(dt, g.world, &dpad, g.codec, g.pluginCanvas)
		}

		if dpad != 0 || action.Action != 0 {
			//go func() {
			n, err := g.codec.Write(common.Message{Dpad: dpad, Action: action.Action})
			if err != nil {
				log.Fatalln("codc write dpad", err)
			}
			g.stats.netsent += n
			//}()
			//g.pos = g.pos.Add(dir.Scaled(10 * dt))
			// g.pos.X = math.Floor(g.pos.X)
			// g.pos.Y = math.Floor(g.pos.Y)
			//log.Println("MOVING PLAYER TO:", pos)
		}

		g.controls.dpad.Store(dpad)

		if dpad != 0 {
			xy := (pos.Add(common.DIR(dpad).Vec().Scaled(playerSpeed)))
			g.me.MoveTo([2]float64{xy.X, xy.Y})
			g.world.Update(g.me)
		}

		if g.win.JustPressed(pixelgl.KeyPageDown) {
			g.controls.paging += PageAmount
			g.flashMessage("Paging: %2.0f", g.controls.paging)
		}
		if g.win.JustPressed(pixelgl.KeyPageUp) {
			g.controls.paging -= PageAmount
			g.flashMessage("Paging: %2.0f", g.controls.paging)
		}
		if g.win.Pressed(pixelgl.KeyPageDown) {
			g.controls.paging += 10 * PageAmount * dt
		}
		if g.win.Pressed(pixelgl.KeyPageUp) {
			g.controls.paging -= 10 * PageAmount * dt
		}
		if g.win.Pressed(pixelgl.KeyLeft) {
			g.cam.camPos.X -= g.cam.camSpeed * dt
		}
		if g.win.Pressed(pixelgl.KeyRight) {
			g.cam.camPos.X += g.cam.camSpeed * dt
		}
		if g.win.Pressed(pixelgl.KeyDown) {
			g.cam.camPos.Y -= g.cam.camSpeed * dt
		}
		if g.win.Pressed(pixelgl.KeyUp) {
			g.cam.camPos.Y += g.cam.camSpeed * dt
		}
		if g.win.JustPressed(pixelgl.KeyTab) {
			g.settings.showChat = !g.settings.showChat
			g.flashMessage("ShowChat: %v", g.settings.showChat)
		}
		// toggles
		if g.win.JustPressed(pixelgl.Key0) {
			g.settings.showWireframe = !g.settings.showWireframe
			g.flashMessage("ShowWireframe: %v", g.settings.showWireframe)
		}
		if g.win.JustPressed(pixelgl.Key1) {
			g.win.SetVSync(!g.win.VSync())
			g.flashMessage("VSync: %v", g.win.VSync())
		}
		if g.win.JustPressed(pixelgl.Key4) {
			g.settings.Debug = !g.settings.Debug
			codec.Debug = g.settings.Debug
			g.flashMessage("Debug: %v", g.settings.Debug)
		}
		if g.win.JustPressed(pixelgl.Key5) {
			g.flashMessage("Moving %s to %2.2f, %2.2f", pos, g.me.X(), g.me.Y())
			pos.X = g.me.X()
			pos.Y = g.me.Y()
		}
		if g.win.JustPressed(pixelgl.Key6) {
			g.settings.SortThings = !g.settings.SortThings
			g.flashMessage("Sorting Things: %v", g.settings.SortThings)
		}
		if g.win.JustPressed(pixelgl.Key7) {
			g.settings.showLand = !g.settings.showLand
			g.flashMessage("ShowLand: %v", g.settings.showLand)
		}
		if g.win.JustPressed(pixelgl.Key8) {
			g.settings.showEntities = !g.settings.showEntities
			g.flashMessage("ShowEntities: %v", g.settings.showEntities)
		}
		if g.win.JustPressed(pixelgl.Key9) {
			g.settings.showPlayerText = !g.settings.showPlayerText
			g.flashMessage("ShowPlayerText: %v", g.settings.showPlayerText)
		}
		if g.win.JustPressed(pixelgl.KeyGraveAccent) {
			// rebuild
			g.flashMessage("recompiling in background...")
			go func() {
				if err := updater.Rebuild(); err != nil {
					g.flashMessageToChat("error building: %v", err)
					return
				}
				g.flashMessage("compiled, relaunching!", err)

				updater.Stage2() // calls syscall.Exec on linux, we go bye bye
				panic("not updated")
			}()
		}
		if g.win.JustPressed(pixelgl.KeyEqual) {
			g.settings.camlock = !g.settings.camlock
			g.flashMessage("Camlock: %v", g.settings.camlock)
		}

	}

	if g.settings.camlock {
		g.cam.camPos = pixel.Lerp(g.cam.camPos, being2vec(g.me), 0.2)
		//g.cam.camPos = being2vec(g.pos)
	}

	// end of dpad

	return false, nil
}

func (g *Game) handleChatConsole() {
	if g.inputbuf.Len() == 1 {
		g.inputbuf.Reset()
		g.settings.typing = false
		return
	}
	fe := strings.Fields(strings.TrimPrefix(g.inputbuf.String(), "/"))
	// slash /commands in chat
	switch fe[0] {
	case "loadplugin":
		if len(fe) != 2 {
			g.flashMessage("need plugin path to load")
			return
		}
		if err := g.loadPlugin(fe[1]); err != nil {
			g.flashMessage("Error loading plugin: %v", err)
			return
		}

	case "help":
		fmt.Fprintf(&g.chattxtbuffer, helpModeText, g.playerid)
		g.controls.paging += 15 * PageAmount
		return
	case "channel":
		g.chatcrypt.Reload(strings.Join(fe[1:], " "))
	case "msg":
		topl, err := strconv.ParseUint(fe[1], 10, 64)
		if err != nil {
			g.stats.displaymsg = 0
			g.statustxtbuf.Reset()
			fmt.Fprintf(&g.statustxtbuf, "error: %v\n", err)
			return
		}

		msg := strings.Join(fe[2:], " ")
		log.Println("Sending typed:", msg)
		n, err := g.codec.Write(common.PlayerMessage{From: g.playerid, To: topl, Message: basex.Encode(g.chatcrypt.Encrypt([]byte(msg)))})
		if err != nil {
			log.Fatalln(err)
		}
		g.stats.netsent += n
	case "tick":
		if len(fe) != 2 {
			g.stats.displaymsg = 0
			g.statustxtbuf.Reset()
			fmt.Fprintf(&g.statustxtbuf, "need 1 arg\n")
			return
		}
		dur, err := time.ParseDuration(fe[1])
		if err != nil {
			g.flashMessage("error parsing duration: %v", err)
			return
		}
		n, err := g.codec.Write(common.ServerUpdate{UpdateTick: dur})
		if err != nil {
			log.Fatalln(err)
		}
		g.stats.netsent += n
		log.Println("sent update", dur.String())
	default:
		g.stats.displaymsg = 0
		g.statustxtbuf.Reset()
		fmt.Fprintf(&g.statustxtbuf, "command invalid: %q\n", fe)
	}
}
