package main

import (
	"errors"
	"fmt"
	"github.com/faiface/pixel"
	"github.com/faiface/pixel/pixelgl"
	"gitlab.com/g4me92bd777b8b16ed4c/common"
	"gitlab.com/g4me92bd777b8b16ed4c/common/codec"
	"gitlab.com/g4me92bd777b8b16ed4c/common/types"
	"gitlab.com/g4me92bd777b8b16ed4c/common/updater"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
)

func (g *Game) controldpad(dt float64) (continueNext bool, err error) {
	// pos.X = gme.X()
	// 		pos.Y = me.Y()

	win := g.win
	dir := pixel.ZV
	dpad := byte(0)

	pos := pixel.V(g.me.X(), g.me.Y())

	inputbuf := g.inputbuf
	if win.JustPressed(pixelgl.KeyEscape) || (win.JustPressed(pixelgl.KeyQ) && win.Pressed(pixelgl.KeyLeftControl)) {
		return false, errors.New("quit")
	}

	if g.settings.typing && win.JustPressed(pixelgl.KeyBackspace) {
		if g.inputbuf.Len() != 0 {
			g.inputbuf.Truncate(g.inputbuf.Len() - 1)
		}
	}
	if g.settings.typing {
		fmt.Fprintf(&g.inputbuf, "%s", win.Typed())
		//	log.Println("Typing:", inputbuf.String())
	}
	if !g.settings.typing && win.JustPressed(pixelgl.KeySlash) {
		g.settings.typing = true
		fmt.Fprintf(&g.inputbuf, "%s", "/")
	}
	if win.JustPressed(pixelgl.KeyEnter) {
		g.settings.typing = !g.settings.typing
		if !g.settings.typing && inputbuf.Len() != 0 {
			if strings.HasPrefix(inputbuf.String(), "/") {
				g.handleChatConsole()
			} else {
				log.Println("Sending typed:", inputbuf.String())
				n, err := g.codec.Write(common.PlayerMessage{From: g.playerid, To: 0, Message: basex.Encode(g.chatcrypt.Encrypt(inputbuf.Bytes()))})
				if err != nil {
					log.Fatalln(err)
				}
				g.stats.netsent += n
			}
			inputbuf.Reset()

		}
	}
	if !g.settings.typing && inputbuf.Len() != 0 {
		inputbuf.Reset()
	}

	if !g.settings.typing {
		if win.Pressed(pixelgl.KeyW) {
			dir.Y += 1
			dpad |= (common.UP)
		}
		if win.Pressed(pixelgl.KeyS) {
			dir.Y -= 1
			dpad |= (common.DOWN)
		}
		if win.Pressed(pixelgl.KeyA) {
			dir.X -= 1
			dpad |= (common.LEFT)
		}
		if win.Pressed(pixelgl.KeyD) {
			dir.X += 1
			dpad |= (common.RIGHT)
		}
		angle2dpad := func(v pixel.Vec) byte {
			v.X = math.Floor(v.X)
			v.Y = math.Floor(v.Y)
			log.Println("Angle2dpad:", v)
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
			if win.Pressed(pixelgl.MouseButtonRight) {
				dpad = angle2dpad(win.MousePosition().Sub(win.Bounds().Center()).Unit().Add(pixel.V(0.4, 0.4)))
				log.Println("MOUSE DIR:", common.DPAD(dpad))
			}
		}
		// if dpad == 0 || fps % 10 == 0 {
		// 	//xpos := g.world.Get(g.playerid)
		// 	//pos = pixel.Lerp(pos, pixel.V(xpos.X(), xpos.Y()), 0.5)
		// }
		action := common.PlayerAction{}
		if win.JustPressed(pixelgl.KeySpace) {
			action.Action = types.ActionManastorm.Uint16()
			g.animations.Push(types.ActionManastorm, pos)
			g.flashMessage("Manastorm!")
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
		//g.spritematrices[g.playerid] = pixel.IM.Scaled(pixel.ZV, 4).Moved(pos)
		g.controls.dpad.Store(dpad)
		if dpad != 0 {
			xy := (pos.Add(common.DIR(dpad).Vec().Scaled(1)))
			x, y := xy.X, xy.Y
			g.me.MoveTo([2]float64{x, y})
			g.world.Update(g.me)
		}

		if g.win.JustPressed(pixelgl.KeyPageDown) {
			g.controls.paging += PageAmount
		}
		if g.win.JustPressed(pixelgl.KeyPageUp) {
			g.controls.paging -= PageAmount
		}
		if win.Pressed(pixelgl.KeyLeft) {
			g.camPos.X -= g.camSpeed * dt
		}
		if win.Pressed(pixelgl.KeyRight) {
			g.camPos.X += g.camSpeed * dt
		}
		if win.Pressed(pixelgl.KeyDown) {
			g.camPos.Y -= g.camSpeed * dt
		}
		if win.Pressed(pixelgl.KeyUp) {
			g.camPos.Y += g.camSpeed * dt
		}
		if win.JustPressed(pixelgl.KeyTab) {
			g.settings.showChat = !g.settings.showChat
			g.flashMessage("ShowChat: %v", g.settings.showChat)
		}
		// toggles
		if win.JustPressed(pixelgl.Key0) {
			g.settings.showWireframe = !g.settings.showWireframe
			g.flashMessage("ShowWireframe: %v", g.settings.showWireframe)
		}
		if win.JustPressed(pixelgl.Key1) {
			win.SetVSync(!win.VSync())
			g.flashMessage("VSync: %v", win.VSync())
		}
		// if win.JustPressed(pixelgl.Key2) {
		// 	g.muted = !g.muted
		// 	if g.muted {
		// 		g.mutechan <- struct{}{}
		// 		g.flashMessage("Muted!")
		// 	}
		// 	if !g.muted {
		// 		go g.soundtrack(g.nextchan, g.mutechan)
		// 		g.flashMessage("Unmuted!")
		// 	}
		// }
		// if win.JustPressed(pixelgl.Key3) {
		// 	g.nextchan <- struct{}{}
		// 	g.flashMessage("Next Song!")
		// }
		if win.JustPressed(pixelgl.Key4) {
			g.settings.Debug = !g.settings.Debug
			codec.Debug = g.settings.Debug
			g.flashMessage("Debug: %v", g.settings.Debug)
		}
		if win.JustPressed(pixelgl.Key5) {
			g.flashMessage("Moving %s to %2.0f, %2.0f", pos, g.me.X(), g.me.Y())
			pos.X = g.me.X()
			pos.Y = g.me.Y()
		}
		if win.JustPressed(pixelgl.Key6) {
			g.settings.SortThings = !g.settings.SortThings
			g.flashMessage("Sorting Things: %v", g.settings.SortThings)
		}
		if win.JustPressed(pixelgl.KeyGraveAccent) {
			// rebuild
			//
			// if err := updater.Rebuild(); err != nil {
			// 	fmt.Fprintf(inputbuf, "ERROR: %v\n", err)
			// 	continue
			// }

			//g.conn.Close() // mystery

			updater.Stage2() // calls syscall.Exec on linux, we go bye bye
			panic("not updated")
		}
		if win.JustPressed(pixelgl.KeyEqual) {
			g.settings.camlock = !g.settings.camlock
			g.flashMessage("Camlock: %v", g.settings.camlock)
		}

	}

	if g.settings.camlock {
		g.camPos = pixel.Lerp(g.camPos, being2vec(g.me), 0.5)
		//g.camPos = being2vec(g.pos)
	}
	return false, nil
}

func (g *Game) handleChatConsole() {
	inputbuf := g.inputbuf
	if inputbuf.Len() == 1 {
		inputbuf.Reset()
		g.settings.typing = false
		return
	}
	fe := strings.Fields(strings.TrimPrefix(g.inputbuf.String(), "/"))
	// slash /commands in chat
	switch fe[0] {
	case "help":
		fmt.Fprintf(&g.chattxtbuffer, "%s\n\n", helpModeText)
		g.controls.paging += 9 * PageAmount
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
		fmt.Fprintf(&g.statustxtbuf, "%q\n", fe)
	}
}
