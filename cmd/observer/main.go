package main

import (
	"bytes"
	"flag"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"

	"github.com/faiface/beep/speaker"
	"gitlab.com/aerth/x/hash/argon2id"
	"gitlab.com/g4me92bd777b8b16ed4c/assets"
	"gitlab.com/g4me92bd777b8b16ed4c/common"
	"gitlab.com/g4me92bd777b8b16ed4c/common/chatenc"
	"gitlab.com/g4me92bd777b8b16ed4c/common/chatstack"
	"gitlab.com/g4me92bd777b8b16ed4c/common/codec"
	"gitlab.com/g4me92bd777b8b16ed4c/common/types"
	"gitlab.com/g4me92bd777b8b16ed4c/common/updater"
	"golang.org/x/image/colornames"
	"image/color"
	"image/color/palette"
	_ "image/png"
	"log"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"net"
	"sync"

	"time"

	"fmt"

	"os"

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/pixelgl"
	"github.com/faiface/pixel/text"
	basexlib "gitlab.com/aerth/x/encoding/basex"
	worldpkg "gitlab.com/g4me92bd777b8b16ed4c/common/world"
)

var (
	Version            string
	DefaultEndpoint    = ""
	defaultChatChannel = "1" // hashed into private key for e2e group chat .. lol
) // set by makefile / ldflags

var basex = basexlib.NewAlphabet(basexlib.Base58Bitcoin)

func init() {
	worldpkg.Type = types.World.Byte()
}

func main() {

	// read command line flags

	log.SetFlags(log.Lshortfile | log.Lmicroseconds)
	// NOTE FROM THE AUTHOR: NOT SECURE CRYPTO, JUST RAND FOR GAMES
	now := time.Now().UnixNano() ^ time.Now().UnixNano()<<32
	xored := int64(now ^ now<<32)
	rand.Seed(xored) // random enough seed
	playerid := rand.Uint64()
	password := ""
	flag.StringVar(&DefaultEndpoint, "s", DefaultEndpoint, "endpoint")
	flag.Uint64Var(&playerid, "p", playerid, "endpoint")
	flag.Int64Var(&xored, "seed", xored, "seed to use for fast random gen") // TODO: remove one day
	flag.StringVar(&defaultChatChannel, "chatcrypt", "", "chatcrypt seed for secure messaging")
	flag.StringVar(&password, "pass", "", "endpoint")
	flag.Parse()
	rand.Seed(xored) // random enough seed
	if playerid == 0 {
		playerid = rand.Uint64()
	}
	log.Printf("generated\n playerid=%016x\n      now=%016x\n      xor=%016x", playerid, now, xored)
	xored = rand.Int63() // yeah
	log.Printf("Helo player %d", playerid)
	hashedpassword := argon2id.New(1, 24, 1).Sum([]byte(password))
	password = ""
	var (
		conn *net.TCPConn
		game *Game
		err  error
	)
	log.Println("Connecting!")
	// connect to server
	conn, err = common.Connect(playerid, DefaultEndpoint, &common.ConnectOptions{})
	if err != nil {
		log.Fatalln(err)
	}
	log.Println("Connected to:", conn.RemoteAddr())

	// create empty world
	game = &Game{
		conn:           conn,
		world:          worldpkg.New(), // holds position of rectangles
		codec:          codec.NewCodec(common.Endian(), conn),
		playerid:       playerid,
		sprites:        make(map[types.Type]*pixel.Sprite),
		spriteframes:   make(map[types.Type]map[byte][]pixel.Rect),
		spritematrices: make(map[uint64]pixel.Matrix),
		chatbuf:        chatstack.New(),
		chatcrypt:      chatenc.Load("hello world, 123 451"),
		statustxtbuf:   &bytes.Buffer{},
	}
	game.spriteframes[types.Human] = make(map[byte][]pixel.Rect)
	for i := byte(0); i < common.ALLDIR; i++ {
		game.spriteframes[types.Human][i] = []pixel.Rect{
			pixel.R(0, 360, 20, 390),
			pixel.R(0, 90, 20, 120),
		}
	}

	spritesheet, err := loadPicture("spritesheet/link.png")
	if err != nil {
		panic(err)
	}
	game.spritesheet = spritesheet

	tree := pixel.NewSprite(spritesheet, game.spriteframes[types.Human][common.DOWN][0])
	game.sprites[types.Human] = tree
	game.spritematrices[game.playerid] = pixel.IM.Scaled(pixel.ZV, 4)
	game.dpad = new(atomic.Value)
	game.dpad.Store(common.DOWN)

	n, err := game.codec.Write(common.Login{ID: playerid, Password: hashedpassword})
	if err != nil {
		log.Fatalln(err)
	}
	game.netsent += n
	log.Println("sent", n, "bytes for login")
	killsound := make(chan struct{})
	nextsound := make(chan struct{})
	go game.soundtrack(nextsound, killsound)
	go game.readloop()
	go game.writeloop()
	game.mutechan = killsound
	game.nextchan = killsound
	game.muted = false
	pixelgl.Run(game.mainloop)
}

type Game struct {
	conn              *net.TCPConn
	codec             *codec.Codec
	world             *worldpkg.World
	playerid          uint64
	spritesheet       pixel.Picture
	sprites           map[types.Type]*pixel.Sprite
	spriteframes      map[types.Type]map[byte][]pixel.Rect
	spritematrices    map[uint64]pixel.Matrix
	dpad              *atomic.Value
	camlock           bool
	chatbuf           *chatstack.ChatStack
	chattxt           *text.Text
	chatcrypt         *chatenc.Crypt
	win               *pixelgl.Window
	chattxtbuffer     bytes.Buffer
	paging            float64
	pageselect        byte // shouldnt overflow
	ping              int64
	maplock           sync.Mutex
	statustxtbuf      *bytes.Buffer
	netsent, netrecvd int
	mutechan          chan struct{}
	nextchan          chan struct{}
	muted             bool
	Debug             bool
}

func (g *Game) soundtrack(next, kill chan struct{}) {

	songs := []string{"music/link2past.mp3", "music/towntheme.mp3"}
	defer g.flashMessage("sound killed at %s", time.Now())
	muted := false
	done := make(chan bool)
	for {

		for _, songname := range songs {
			f, err := assets.Assets.Open(songname)
			if err != nil {
				log.Fatalln(err)
			}

			streamer, format, err := mp3.Decode(f)
			if err != nil {
				log.Fatal(err)
			}

			speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
			speaker.Play(beep.Seq(streamer, beep.Callback(func() {
				done <- true
			})))
			for {
				select {
				case <-next:
					g.flashMessage("Skipping song")
					f.Close()
					streamer.Close()
					continue
				case <-kill:
					muted = !muted
					g.flashMessage("Muted: %v", muted)
					if muted {
						speaker.Clear()

					} else {
						speaker.Play(beep.Seq(streamer, beep.Callback(func() {
							done <- true
						})))
					}
				case <-done:
					g.flashMessage("Skipping song")
					f.Close()
					streamer.Close()
					continue
				}
			}
			streamer.Close()
		}

	}
}

type Player struct {
	sprite *pixel.Sprite
	PID    uint64
	pos    pixel.Vec
}

func (p Player) ID() uint64 {
	return p.PID
}

func (p Player) X() float64 {
	return p.pos.X
}

func (p Player) Y() float64 {
	return p.pos.Y
}

func (p Player) Type() byte {
	return byte(types.Player)
}
func (p Player) Pos() pixel.Vec {
	return p.pos
}

var PageAmount = 24.0

func (g *Game) flashMessage(f string, i ...interface{}) {
	//g.displaymsg = 0
	g.statustxtbuf.Reset()
	fmt.Fprintf(g.statustxtbuf, f, i...)
}
func (g *Game) mainloop() {
	var (
		cfg   pixelgl.WindowConfig
		win   *pixelgl.Window
		batch *pixel.Batch
		err   error
	)

	cfg = pixelgl.WindowConfig{
		Title:     "Game Title",
		Bounds:    pixel.R(0, 0, 700, 500),
		VSync:     true,
		Resizable: true,
	}

	win, err = pixelgl.NewWindow(cfg)
	if err != nil {
		panic(err)
	}

	g.win = win
	batch = pixel.NewBatch(&pixel.TrianglesData{}, g.spritesheet)
	var (
		camPos       = pixel.ZV
		camSpeed     = 500.0
		camZoom      = 1.0
		camZoomSpeed = 1.2
	)

	last := time.Now()

	//pos := pixel.ZV
	mkcolor := func(i uint64) color.Color {
		col := colors[i%uint64(len(colors))]
		c := col.(color.RGBA)
		c.A = 255
		return c
	}
	var typing = false
	var txtbar = mkfont(0, 22.0, "")
	var inputbuf = new(bytes.Buffer)
	// [0] is player

	spritesheet2, err := loadPicture("spritesheet/overworld_tileset.png")
	if err != nil {
		log.Fatalln(err)
	}

	sprites2 := []*pixel.Sprite{}
	matrix2 := []pixel.Matrix{}
	spritesheet2frames := map[string]pixel.Rect{
		"X": pixel.R(272, 225, 272+16, 225+16),
	}
	statustxt := mkfont(0, 24.0, "font/computer-font.ttf")
	second := time.Tick(time.Second)
	fps := 0
	lastdt := 0.0
	showChat := true
	lastfps := 0
	lastnetsend := 0
	lastnetrecv := 0
	me := g.world.Get(g.playerid)
	if me == nil {
		g.world.Update(&Player{PID: g.playerid, pos: pixel.ZV})
		me = g.world.Get(g.playerid)
	}
	g.camlock = true
	if g.sprites[types.Human] == nil {
		tree := pixel.NewSprite(g.spritesheet, g.spriteframes[types.Human][common.DOWN][rand.Intn(len(g.spriteframes[types.Human][common.DOWN]))])
		g.sprites[types.Human] = tree
	}
	displaymsg := 0
	var ui *pixel.Batch
	uip, err := loadPicture("spritesheet/overworld_tileset.png")
	if err != nil {
		log.Fatalln(err)
	}
	uiframes := map[string]pixel.Rect{
		"X":      pixel.R(272, 225, 272+16, 225+16),
		"stone":  pixel.R(86.5, 379, 86.5+16, 379+16),
		"stone2": pixel.R(86.5-16, 379, 86.5, 379+16),
		"blue":   pixel.R(0, 0, 16, 16),
		"grass":  pixel.R(222, 427-169, 222+15, 427-169+15),
	}
	background := pixel.NewBatch(&pixel.TrianglesData{}, uip)
	grass := pixel.NewSprite(uip, uiframes["grass"])
	for xy := 0.0; xy <= win.Bounds().H()+32; xy = xy + 32 {
		for xc := 0.0; xc <= win.Bounds().W()+32; xc = xc + 32 {
			grass.Draw(background, pixel.IM.Scaled(pixel.ZV, 2).Moved(pixel.V(xc, xy)))
		}
	}

	ui = pixel.NewBatch(&pixel.TrianglesData{}, uip)
	dash := pixel.NewSprite(uip, uiframes["blue"])
	semitr := color.Alpha16{0xaaaa}
	for xc := 0.0; xc <= 2*win.Bounds().W()+16*4; xc = xc + 16*4 {
		dash.DrawColorMask(ui, pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(xc, 0)), semitr)
	}
	world := g.world.DeepCopy()
	xpos := g.world.Get(g.playerid)
	pos := pixel.V(xpos.X(), xpos.Y())
	showWireframe := false
	fullscreen := false
	for !win.Closed() {
		dt := time.Since(last).Seconds()
		last = time.Now()
		fps++
		cam := pixel.IM.Scaled(camPos, camZoom).Moved(win.Bounds().Center().Sub(camPos))

		win.SetMatrix(cam)
		me = g.world.Get(g.playerid)
		if win.JustPressed(pixelgl.KeyF) && win.Pressed(pixelgl.KeyLeftControl) {
			fullscreen = !fullscreen
			if fullscreen {
				win.SetMonitor(pixelgl.PrimaryMonitor())
			} else {
				win.SetMonitor(nil)
			}
		}

		me = world.Get(g.playerid)
		typin := "false"
		if typing {
			typin = inputbuf.String()
		}
		statustxt.Clear()
		g.maplock.Lock()
		worldlen := g.world.Len()
		fmt.Fprintf(statustxt, ""+
			"DT=%.0fms FPS=%d PING=%dms PLAYERS=(%d online) VERSION=%s\n"+
			"NET SENT=%04db/s RECV=%06d b/s GPS=%03.0f,%03.0f TYPING=%q\n%s",
			lastdt*1000, lastfps, g.ping, worldlen, Version,
			lastnetsend, lastnetrecv, pos.X, pos.Y, typin, g.statustxtbuf.String(),
		)

		g.maplock.Unlock()

		select {
		default:
		case <-second:
			lastfps = fps
			fps = 0
			lastdt = dt
			lastnetrecv = g.netrecvd
			lastnetsend = g.netsent
			g.netsent = 0
			g.netrecvd = 0
			displaymsg++
			if displaymsg > 5 { // show message for 5 seconds
				displaymsg = 0
				g.statustxtbuf.Reset()
			}

		}
		if win.JustPressed(pixelgl.MouseButtonLeft) {
			g.maplock.Lock()
			x := pixel.NewSprite(spritesheet2, spritesheet2frames["X"])

			sprites2 = append(sprites2, x)
			mouse := cam.Unproject(win.MousePosition())
			log.Println("Put marker:", mouse)
			matrix2 = append(matrix2, pixel.IM.Scaled(pixel.ZV, 4).Moved(mouse))
			g.maplock.Unlock()
		}

		if win.JustPressed(pixelgl.KeyEscape) || (win.JustPressed(pixelgl.KeyQ) && win.Pressed(pixelgl.KeyLeftControl)) {
			win.Destroy()
			return
		}

		dir := pixel.ZV
		playerSpeed := 100.0 * dt
		var dpad byte

		if typing && win.JustPressed(pixelgl.KeyBackspace) {
			if inputbuf.Len() != 0 {
				inputbuf.Truncate(inputbuf.Len() - 1)
			}
		}
		if typing {
			fmt.Fprintf(inputbuf, "%s", win.Typed())
			//	log.Println("Typing:", inputbuf.String())
		}
		if !typing && win.JustPressed(pixelgl.KeySlash) {
			typing = true
			fmt.Fprintf(inputbuf, "%s", "/")
		}
		if win.JustPressed(pixelgl.KeyEnter) {
			typing = !typing
			if !typing && inputbuf.Len() != 0 {
				if strings.HasPrefix(inputbuf.String(), "/") {
					if inputbuf.Len() == 1 {
						inputbuf.Reset()
						typing = false
						continue
					}
					fe := strings.Fields(strings.TrimPrefix(inputbuf.String(), "/"))
					// slash /commands in chat
					switch fe[0] {
					case "channel":
						g.chatcrypt.Reload(strings.Join(fe[1:], " "))
					case "msg":
						topl, err := strconv.ParseUint(fe[1], 10, 64)
						if err != nil {
							displaymsg = 0
							g.statustxtbuf.Reset()
							fmt.Fprintf(g.statustxtbuf, "error: %v\n", err)
							continue
						}

						msg := strings.Join(fe[2:], " ")
						log.Println("Sending typed:", msg)
						n, err := g.codec.Write(common.PlayerMessage{From: g.playerid, To: topl, Message: basex.Encode(g.chatcrypt.Encrypt([]byte(msg)))})
						if err != nil {
							log.Fatalln(err)
						}
						g.netsent += n
					case "tick":
						if len(fe) != 2 {
							displaymsg = 0
							g.statustxtbuf.Reset()
							fmt.Fprintf(g.statustxtbuf, "need 1 arg\n")
							continue
						}
						dur, err := time.ParseDuration(fe[1])
						if err != nil {
							g.flashMessage("error parsing duration: %v", err)
							continue
						}
						n, err := g.codec.Write(common.ServerUpdate{UpdateTick: dur})
						if err != nil {
							log.Fatalln(err)
						}
						g.netsent += n
						log.Println("sent update", dur.String())
					default:
						displaymsg = 0
						g.statustxtbuf.Reset()
						fmt.Fprintf(g.statustxtbuf, "%q\n", fe)
					}
				} else {
					log.Println("Sending typed:", inputbuf.String())
					n, err := g.codec.Write(common.PlayerMessage{From: g.playerid, To: 0, Message: basex.Encode(g.chatcrypt.Encrypt(inputbuf.Bytes()))})
					if err != nil {
						log.Fatalln(err)
					}
					g.netsent += n
				}
				inputbuf.Reset()

			}
		}

		if !typing {
			if win.Pressed(pixelgl.KeyW) {
				dir.Y += 1 * playerSpeed
				dpad |= (common.UP)
			}
			if win.Pressed(pixelgl.KeyS) {
				dir.Y -= 1 * playerSpeed
				dpad |= (common.DOWN)
			}
			if win.Pressed(pixelgl.KeyA) {
				dir.X -= 1 * playerSpeed
				dpad |= (common.LEFT)
			}
			if win.Pressed(pixelgl.KeyD) {
				dir.X += 1 * playerSpeed
				dpad |= (common.RIGHT)
			}
			angle2dpad := func(v pixel.Vec) byte {
				switch v {
				case pixel.V(0, 1):
					return common.UP
				case pixel.V(1, 1):
					return common.UPRIGHT
				case pixel.V(-1, 1):
					return common.LEFT
				case pixel.V(1, 0):
					return common.DOWN
				case pixel.V(1, 0):
					return common.DOWNRIGHT
				case pixel.V(-1, 0):
					return common.DOWNLEFT
				case pixel.V(1, 0):
					return common.RIGHT
				case pixel.V(-1, 0):
					return common.LEFT
				default:
					log.Println("UNKONWN ANGLE:", v)
					log.Println(dpad)
					return 0
				}
			}
			if dpad == 0 {
				if win.Pressed(pixelgl.MouseButtonRight) {
					angle := win.Bounds().Center().Sub(win.MousePosition()).Map(func(f float64) float64 {
						if f >= 1 {
							return 1
						}
						return 0
					})
					log.Println("MOUSE DIR:", angle)
					dpad = angle2dpad(angle)
				}
			}
			// if dpad == 0 || fps % 10 == 0 {
			// 	//xpos := g.world.Get(g.playerid)
			// 	//pos = pixel.Lerp(pos, pixel.V(xpos.X(), xpos.Y()), 0.5)
			// }
			if dpad != 0 {
				n, err := g.codec.Write(common.Message{Dpad: dpad})
				if err != nil {
					log.Fatalln("codc write dpad", err)
				}
				g.netsent += n
				pos = pos.Add(dir.Scaled(1))
				//log.Println("MOVING PLAYER TO:", pos)

			}
			//g.spritematrices[g.playerid] = pixel.IM.Scaled(pixel.ZV, 4).Moved(pos)
			g.dpad.Store(dpad)
			if dpad != 0 {
				g.world.Update(&Player{PID: g.playerid, pos: pos})
			}

			if g.win.JustPressed(pixelgl.KeyPageDown) {
				g.paging += PageAmount
			}
			if g.win.JustPressed(pixelgl.KeyPageUp) {
				g.paging -= PageAmount
			}
			if win.Pressed(pixelgl.KeyLeft) {
				camPos.X -= camSpeed * dt
			}
			if win.Pressed(pixelgl.KeyRight) {
				camPos.X += camSpeed * dt
			}
			if win.Pressed(pixelgl.KeyDown) {
				camPos.Y -= camSpeed * dt
			}
			if win.Pressed(pixelgl.KeyUp) {
				camPos.Y += camSpeed * dt
			}
			if win.JustPressed(pixelgl.KeyTab) {
				showChat = !showChat
				g.flashMessage("ShowChat: %v", showChat)
			}
			// toggles
			if win.JustPressed(pixelgl.Key0) {
				showWireframe = !showWireframe
				g.flashMessage("ShowWireframe: %v", showWireframe)
			}
			if win.JustPressed(pixelgl.Key1) {
				win.SetVSync(!win.VSync())
				g.flashMessage("VSync: %v", win.VSync())
			}
			if win.JustPressed(pixelgl.Key2) {
				g.muted = !g.muted
				if g.muted {
					g.mutechan <- struct{}{}
					g.flashMessage("Muted!")
				}
				if !g.muted {
					go g.soundtrack(g.nextchan, g.mutechan)
					g.flashMessage("Unmuted!")
				}
			}
			if win.JustPressed(pixelgl.Key3) {
				g.nextchan <- struct{}{}
				g.flashMessage("Next Song!")
			}
			if win.JustPressed(pixelgl.Key4) {
				g.Debug = !g.Debug
				codec.Debug = g.Debug
				g.flashMessage("Debug: %v", g.Debug)
			}
			if win.JustPressed(pixelgl.Key5) {
				g.flashMessage("Moving %s to %s", pos, me)
				pos.X = me.X()
				pos.Y = me.Y()
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
				g.camlock = !g.camlock
				g.flashMessage("Camlock: %v", g.camlock)
			}
		}
		if g.camlock {
			camPos = pos
		}

		camZoom *= math.Pow(camZoomSpeed, win.MouseScroll().Y)

		win.Clear(colornames.Forestgreen)

		//	background.Draw(g.win)
		g.win.SetMatrix(cam)

		if g.chattxt == nil {
			g.chattxt = mkfont(0, PageAmount, "")
			g.chattxt.Color = colornames.Black
		}

		g.win.SetMatrix(pixel.IM)
		var msg chatstack.ChatMessage
		for i := 0; i < 10; i++ {
			msg = g.chatbuf.Pop()
			if msg.Message == "" {
				break
			}

			log.Println("Popped chat messages:", i+1)

			from := msg.From
			if len(from) > 5 {
				from = from[:5]
			}
			g.paging += PageAmount
			fmt.Fprintf(&g.chattxtbuffer, "[%s] (%s) %q\n", from, msg.To, msg.Message)
			fmt.Fprintf(os.Stderr, "[%s] (%s) %q\n", from, msg.To, msg.Message)
		}

		g.chattxt.Clear()
		fmt.Fprintf(g.chattxt, "%s", g.chattxtbuffer.String())

		// g.chattxt.Draw(win, pixel.IM.Moved(win.Bounds().Center()))

		if showChat {
			win.SetMatrix(pixel.IM)
			g.chattxt.DrawColorMask(g.win, pixel.IM.Moved(pixel.V(10, g.win.Bounds().H()-300).Add(pixel.V(0, g.paging))), color.RGBA{0xff, 0xff, 0xff, 0xaa})
		}

		win.SetMatrix(pixel.IM.Moved(pixel.V(0, g.win.Bounds().Max.Y-32)))
		ui.Draw(g.win)
		win.SetMatrix(pixel.IM)
		ui.Draw(g.win)
		statustxt.Draw(win, pixel.IM.Moved(pixel.V(2, g.win.Bounds().Max.Y-PageAmount-1)))

		win.SetMatrix(cam)
		batch.Clear()
		sorted := g.world.SnapshotBeings()

		sort.Sort(sorted)
		for i := range sorted {
			v := sorted[len(sorted)-(i+1)]
			id := v.ID()
			// color := colornames.Red
			// color.A = 240
			// if id == g.playerid {
			// 	continue
			// }
			//	g.maplock.Lock()
			g.maplock.Lock()
			if spr, ok := g.sprites[types.Human]; ok {
				if id != g.playerid {
					newpos := being2vec(v)
					if _, ok := g.spritematrices[id]; !ok {
						g.flashMessage("Player logged on: %d\n", id)
						g.spritematrices[id] = pixel.IM.Scaled(pixel.ZV, 4).Moved(newpos)
					} else {
						g.spritematrices[id] = pixel.IM.Scaled(pixel.ZV, 4).Moved(newpos)
					}
					spr.DrawColorMask(batch, g.spritematrices[id], mkcolortransparent(mkcolor(id), 0.5))
				}
			} else {
				log.Fatalln("ouch")
			}
			g.maplock.Unlock()
		}

		//g.sprites[types.Human].DrawColorMask(batch, g.spritematrices[g.playerid], colornames.Green)
		//g.maplock.Lock()
		// draw player on top
		g.spritematrices[g.playerid] = pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(math.Floor(pos.X), math.Floor(pos.Y)))
		//g.maplock.Unlock()
		//g.sprites[types.Human].DrawColorMask(batch, g.spritematrices[g.playerid], colornames.Green)
		g.sprites[types.Human].DrawColorMask(batch, pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(pos.X, pos.Y)), colornames.White)

		batch.Draw(win)
		txtbar.Draw(win, pixel.IM)

		win.Update()
	}
}

var colors = palette.Plan9

func (g *Game) readloop() {
	buf := make([]byte, 1024)
	var logoff = new(common.PlayerLogoff)
	var entities = new(worldpkg.Beings)
	for {
		t, reqid, n, err := g.codec.Read(buf)
		if err != nil {
			log.Fatalln(err)
		}
		g.netrecvd += n
		if g.Debug {
			log.Printf("Got server packet #%d, type %d (%s) %d bytes", reqid, t, types.Type(t).String(), n)
		}

		switch types.Type(t) {
		case types.Ping:
			ping := &common.Ping{}
			if err := ping.Decode(buf[:n]); err != nil {
				log.Fatalln(err)
			}
			g.ping = time.Since(ping.Time).Milliseconds()
		case types.Pong:
			ping := &common.Ping{}
			if err := ping.Decode(buf[:n]); err != nil {
				log.Fatalln(err)
			}
			g.ping = time.Since(ping.Time).Milliseconds()
		case types.UpdateGps:
			log.Printf("UPDATE GPS:     %02x", buf[:n])
		case types.UpdatePlayers:
			log.Printf("UPDATE PLAYERS: %02x", buf[:n])
		case types.PlayerMessage:
			m := &common.PlayerMessage{}
			if err := m.Decode(buf[:n]); err != nil {
				log.Fatalln(err)
			}

			b, err := basex.Decode(m.Message)
			if err == nil {

				msg := g.chatcrypt.Decrypt(b)
				if msg == nil {
					log.Println("COULD NOT DECRYPT")
					//continue
				}
				log.Println("Got message:", m.Message)
				g.chatbuf.Push(chatstack.ChatMessage{
					From:    fmt.Sprintf("%d", m.From),
					To:      fmt.Sprintf("%d", m.To),
					Message: string(msg),
				})
			}
		case types.PlayerLogoff:

			if err := logoff.Decode(buf[:n]); err != nil {
				log.Fatalln(err)
			}
			delete(g.spritematrices, logoff.UID)
			g.world.Remove(logoff.UID)
			g.flashMessage("Player logged off: %d", logoff.UID)
		case types.PlayerAction:
			//	g.maplock.Lock()
			a := common.PlayerAction{}
			if err := g.codec.Decode(buf[:n], &a); err != nil {
				log.Fatalln(err)
			}
			oldpos := g.world.Get(a.ID)
			if oldpos == nil {
				oldpos = &Player{PID: a.ID}
			}
			oldv := pixel.V(float64(oldpos.X()), float64(oldpos.Y()))
			if g.Debug {
				log.Println("got player action from server:", a.ID, "NEWPOS", a.Pos, common.DPAD(a.DPad), "OLD POS", oldv)
			}

			pv := pixel.V(float64(a.Pos[0]), float64(a.Pos[1]))
			g.world.Update(&Player{
				PID: a.ID,
				pos: pv,
			})

			if g.playerid != a.ID {
				g.maplock.Lock()
				g.spritematrices[a.ID] = pixel.IM.Scaled(pixel.ZV, 4).Moved(pv)
				g.maplock.Unlock()
			}

		case types.World:

			if g.Debug {
				log.Printf("UPDATE WORLD:   %02x", buf[:n])
			}
			if err := entities.Decode(buf[:n]); err != nil {
				log.Fatalln("decode world:", err)
			}
			if g.Debug {
				log.Printf("GOT WORLD, len %d, bytes %02x", len(*entities), buf[:n])
			}
			if len(*entities) == 0 {
				log.Fatalln("world is gone")
			}
			g.maplock.Lock()
			for i, v := range *entities {
				if g.Debug {
					log.Printf("NEWMAP: [%d] %d (%02.2f,%02.2f) T=%d (%s) %T", i, v.ID(), v.X(), v.Y(), v.Type(), common.TYPE(v.Type()), v)
				}
				if g.sprites[types.Human] == nil {
					tree := pixel.NewSprite(g.spritesheet, g.spriteframes[types.Human][common.DOWN][rand.Intn(len(g.spriteframes[types.Human][common.DOWN]))])
					g.sprites[types.Human] = tree

				}
				g.world.Update(v)
				g.spritematrices[v.ID()] = pixel.IM.Scaled(pixel.ZV, 4).Moved(being2vec(v))
			}
			g.maplock.Unlock()
			if g.Debug {
				log.Println("END UPDATE WORLD")
			}

		default:
			log.Fatalln("alien packet:", types.Type(t).String())
		}

	}
}

func (g *Game) writeloop() {
	tick := time.Tick(time.Second * 3)

	for _ = range tick {
		n, err := g.codec.Write(common.Ping{ID: g.playerid, Time: time.Now().UTC()})
		if err != nil {
			log.Fatalln("write ping error:", err)
		}
		g.netsent += n
	}

}

func (p *Player) MoveTo(xy [2]float64) {
	p.pos.X = xy[0]
	p.pos.Y = xy[1]
}

func being2vec(b worldpkg.Being) pixel.Vec {
	return pixel.V(b.X(), b.Y())
}

func mkcolortransparent(c color.Color, opacity float64) (out color.RGBA) {
	out.A = uint8(math.Floor(opacity * 255.0))
	r, g, b, _ := c.RGBA()
	out.R, out.G, out.B = uint8(r>>4), uint8(g>>4), uint8(b>>4)
	return
}
