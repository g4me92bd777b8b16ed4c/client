package main

import (
	"bytes"
	"flag"

	"image/color"
	"image/color/palette"
	_ "image/png"
	"log"
	"math"
	"math/rand"
	"sort"
	"sync/atomic"

	"gitlab.com/aerth/x/hash/argon2id"
	"gitlab.com/g4me92bd777b8b16ed4c/common"
	"gitlab.com/g4me92bd777b8b16ed4c/common/chatenc"
	"gitlab.com/g4me92bd777b8b16ed4c/common/chatstack"
	"gitlab.com/g4me92bd777b8b16ed4c/common/codec"
	"gitlab.com/g4me92bd777b8b16ed4c/common/types"
	"golang.org/x/image/colornames"

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

// func init() {
// 	worldpkg.Type = types.World.Byte()
// }

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
	game.spriteframes[types.Player] = make(map[byte][]pixel.Rect)
	for i := byte(0); i < common.ALLDIR; i++ {
		game.spriteframes[types.Player][i] = []pixel.Rect{
			pixel.R(0, 360, 20, 390),
			pixel.R(0, 90, 20, 120),
		}
	}

	spritesheet, err := loadPicture("spritesheet/link.png")
	if err != nil {
		panic(err)
	}
	game.spritesheet = spritesheet

	tree := pixel.NewSprite(spritesheet, game.spriteframes[types.Player][common.DOWN][0])
	game.sprites[types.Player] = tree
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

type RuntimeSettings struct {
	showChat      bool
	showWireframe bool
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
	inputbuf          *bytes.Buffer
	typing            bool
	me                worldpkg.Being
	displaymsg        byte

	settings     RuntimeSettings
	camPos       pixel.Vec
	camSpeed     float64
	camZoom      float64
	camZoomSpeed float64
	cam          pixel.Matrix
	pos          pixel.Vec
	//

}

func (g *Game) soundtrack(next, kill chan struct{}) {

	// songs := []string{"music/link2past.mp3", "music/towntheme.mp3"}
	// defer g.flashMessage("sound killed at %s", time.Now())
	// muted := false
	// done := make(chan bool)
	// for {

	// 	for _, songname := range songs {
	// 		f, err := assets.Assets.Open(songname)
	// 		if err != nil {
	// 			log.Fatalln(err)
	// 		}

	// 		streamer, format, err := mp3.Decode(f)
	// 		if err != nil {
	// 			log.Fatal(err)
	// 		}

	// 		speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10))
	// 		speaker.Play(beep.Seq(streamer, beep.Callback(func() {
	// 			done <- true
	// 		})))
	// 		for {
	// 			select {
	// 			case <-next:
	// 				g.flashMessage("Skipping song")
	// 				f.Close()
	// 				streamer.Close()
	// 				continue
	// 			case <-kill:
	// 				muted = !muted
	// 				g.flashMessage("Muted: %v", muted)
	// 				if muted {
	// 					speaker.Clear()

	// 				} else {
	// 					speaker.Play(beep.Seq(streamer, beep.Callback(func() {
	// 						done <- true
	// 					})))
	// 				}
	// 			case <-done:
	// 				g.flashMessage("Skipping song")
	// 				f.Close()
	// 				streamer.Close()
	// 				continue
	// 			}
	// 		}
	// 		streamer.Close()
	// 	}

	// }
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

func (p Player) Type() types.Type {
	return types.Player
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
		cfg     pixelgl.WindowConfig
		win     *pixelgl.Window
		batches = make(map[types.Type]*pixel.Batch)
		err     error
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

	newbatch := func(t types.Type, pic pixel.Picture) {
		batches[t] = pixel.NewBatch(&pixel.TrianglesData{}, pic)
	}
	newbatch(types.Player, g.spritesheet)
	g.camPos = pixel.ZV
	g.camSpeed = 500.0
	g.camZoom = 1.0
	g.camZoomSpeed = 1.2

	last := time.Now()

	//pos := pixel.ZV
	mkcolor := func(i uint64) color.Color {
		col := colors[i%uint64(len(colors))]
		c := col.(color.RGBA)
		c.A = 255
		return c
	}

	var txtbar = mkfont(0, 22.0, "")
	g.inputbuf = new(bytes.Buffer)
	// [0] is player

	spritesheet2, err := loadPicture("spritesheet/overworld_tileset.png")
	if err != nil {
		log.Fatalln(err)
	}

	//sprites2 := []*pixel.Sprite{}
	//matrix2 := []pixel.Matrix{}
	// spritesheet2frames := map[string]pixel.Rect{
	// 	"X": pixel.R(272, 225, 272+16, 225+16),
	// }
	statustxt := mkfont(0, 24.0, "font/computer-font.ttf")
	second := time.Tick(time.Second)
	fps := 0
	lastdt := 0.0
	showChat := true
	lastfps := 0
	lastnetsend := 0
	lastnetrecv := 0
	landframes := map[types.Type]pixel.Rect{
		types.TileGrass: pixel.R(222, 427-169, 222+15, 427-169+15),
		types.TileWater: pixel.R(222, 427-169, 222+15, 427-169+15),
		types.TileRock:  pixel.R(222, 427-169, 222+15, 427-169+15),
	}
	landsheet := spritesheet2
	me := g.world.Get(g.playerid)
	if me == nil {
		g.world.Update(&Player{PID: g.playerid, pos: pixel.ZV})
		me = g.world.Get(g.playerid)
	}
	g.camlock = true
	if g.sprites[types.Player] == nil {
		tree := pixel.NewSprite(g.spritesheet, g.spriteframes[types.Player][common.DOWN][rand.Intn(len(g.spriteframes[types.Player][common.DOWN]))])
		g.sprites[types.Player] = tree
	}

	if g.sprites[types.TileGrass] == nil {
		tree := pixel.NewSprite(landsheet, landframes[types.TileGrass])
		g.sprites[types.TileGrass] = tree
	}
	if g.sprites[types.TileWater] == nil {
		tree := pixel.NewSprite(landsheet, landframes[types.TileWater])
		g.sprites[types.TileWater] = tree
	}
	if g.sprites[types.TileRock] == nil {
		tree := pixel.NewSprite(landsheet, landframes[types.TileRock])
		g.sprites[types.TileRock] = tree
	}

	newbatch(types.TileGrass, landsheet)
	batches[types.TileRock] = batches[types.TileGrass]
	batches[types.TileWater] = batches[types.TileGrass]

	world := g.world
	themap := []*worldpkg.Tile{}
	for y := 0.0; y < 1000; y = y + 16 {
		for x := 0.0; x < 1000; x = x + 16 {
			world.NewTile(types.TileGrass, x, y)
		}
	}
	for _, t := range themap {
		g.world.SetTile(t)
	}

	t := g.world.GetTile(pixel.V(0, 0))
	switch t.Type {
	case types.TileGrass:
		g.sprites[t.Type].Draw(batches[t.Type], pixel.IM.Moved(t.Pos()))
	default:
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
			grass.DrawColorMask(background, pixel.IM.Scaled(pixel.ZV, 2).Moved(pixel.V(xc, xy)), colornames.Blueviolet)
		}
	}

	ui = pixel.NewBatch(&pixel.TrianglesData{}, uip)
	dash := pixel.NewSprite(uip, uiframes["blue"])
	semitr := color.Alpha16{0xaaaa}
	for xc := 0.0; xc <= 2*win.Bounds().W()+16*4; xc = xc + 16*4 {
		dash.DrawColorMask(ui, pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(xc, 0)), semitr)
	}

	//xpos := g.world.Get(g.playerid)
	pos := pixel.ZV
	fullscreen := false
	clearablebatches := []*pixel.Batch{
		batches[types.Player],
	}
	for !win.Closed() {
		dt := time.Since(last).Seconds()
		//playerSpeed = 100.0 * dt
		last = time.Now()
		fps++
		g.cam = pixel.IM.Scaled(g.camPos, g.camZoom).Moved(win.Bounds().Center().Sub(g.camPos))
		// dpad = 0
		// dir.X = 0
		// dir.Y = 0
		win.SetMatrix(g.cam)
		g.me = g.world.Get(g.playerid)

		if win.JustPressed(pixelgl.KeyF) && win.Pressed(pixelgl.KeyLeftControl) {
			fullscreen = !fullscreen
			if fullscreen {
				win.SetMonitor(pixelgl.PrimaryMonitor())
			} else {
				win.SetMonitor(nil)
			}
		}

		typin := "false"
		if g.typing {
			typin = g.inputbuf.String()
		}
		statustxt.Clear()
		g.maplock.Lock()
		worldlen := g.world.Len()
		fmt.Fprintf(statustxt, ""+
			"DT=%.0fms FPS=%d PING=%dms PLAYERS=(%d online) VERSION=%s\n"+
			"NET SENT=%04db/s RECV=%06d b/s GPS=%03.0f,%03.0f TYPING=%q\n%s",
			lastdt*1000, lastfps, g.ping, worldlen, Version,
			lastnetsend, lastnetrecv, g.pos.X, g.pos.Y, typin, g.statustxtbuf.String(),
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

		cont, err := g.controldpad(dt)
		if err != nil {
			if err.Error() == "quit" {
				win.Destroy()
				os.Exit(0)
			}
			log.Fatalln(err)
		}

		if cont {
			continue
		}

		pos.X = g.me.X()
		pos.Y = g.me.Y()
		g.camZoom *= math.Pow(g.camZoomSpeed, win.MouseScroll().Y)

		win.Clear(colornames.Forestgreen)

		// begin draw
		//background.Draw(g.win)
		g.win.SetMatrix(g.cam)

		if g.chattxt == nil {
			g.chattxt = mkfont(0, PageAmount, "")
			g.chattxt.Color = colornames.Black
		}

		g.win.SetMatrix(pixel.IM)

		// chat

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
		if showChat {
			win.SetMatrix(pixel.IM)
			g.chattxt.DrawColorMask(g.win, pixel.IM.Moved(pixel.V(10, g.win.Bounds().H()-300).Add(pixel.V(0, g.paging))), color.RGBA{0xff, 0xff, 0xff, 0xaa})
		}

		// win.SetMatrix(pixel.IM.Moved(pixel.V(0, g.win.Bounds().Max.Y-32)))
		// ui.Draw(g.win) // upper bar
		// win.SetMatrix(pixel.IM)
		// ui.Draw(g.win) // lower bar
		// draw status txt
		statustxt.Draw(win, pixel.IM.Moved(pixel.V(2, g.win.Bounds().Max.Y-PageAmount-1)))

		win.SetMatrix(g.cam)
		for _, v := range clearablebatches {
			v.Clear()
		}
		sorted := g.world.SnapshotBeings()
		_ = mkcolor
		sort.Sort(sorted)
		//sort.Reverse(sorted)
		spr := g.sprites[types.Player]

		for i := range sorted {

			// v := sorted[len(sorted)-(i+1)]
			// id := v.ID()
			// // color := colornames.Red
			// // color.A = 240
			// // if id == g.playerid {
			// // 	continue
			// // }
			// //	g.maplock.Lock()
			// g.maplock.Lock()
			// spr := g.sprites[types.Player]
			// if id == g.playerid {
			// 	continue
			// }
			//newpos := being2vec(v)
			// if _, ok := g.spritematrices[id]; !ok {
			// 	g.flashMessage("Player logged on: %d\n", id)
			// 	g.spritematrices[id] = pixel.IM.Scaled(pixel.ZV, 4).Moved(newpos)
			// } else {
			// 	g.spritematrices[id] = pixel.IM.Scaled(pixel.ZV, 4).Moved(newpos)
			// }
			//spr.DrawColorMask(batches[types.Player], g.spritematrices[id], mkcolortransparent(mkcolor(id), 0.5))
			spr.Draw(batches[types.Player], pixel.IM.Scaled(pixel.ZV, 4).Moved(being2vec(sorted[i])))
			//	}

		}

		//g.sprites[types.Player].DrawColorMask(batch, g.spritematrices[g.playerid], colornames.Green)
		//g.maplock.Lock()
		// draw player on top
		//	g.spritematrices[g.playerid] = pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(math.Floor(g.pos.X), math.Floor(g.pos.Y)))
		//g.sprites[types.Player].Draw(batches[types.Player], pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(math.Floor(pos.X), math.Floor(pos.Y))))
		//g.sprites[types.Player].Draw(batches[types.Player], pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(math.Floor(g.pos.X), math.Floor(g.pos.Y))))
		//g.maplock.Unlock()
		//g.sprites[types.Player].DrawColorMask(batch, g.spritematrices[g.playerid], colornames.Green)
		//	g.sprites[types.Player].DrawColorMask(batches[types.Player], pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(pos.X, pos.Y)), colornames.White)
		for _, batch := range []*pixel.Batch{batches[types.TileWater], batches[types.TileGrass], batches[types.TileRock], batches[types.Player]} {
			batch.Draw(win)
		}
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
				if g.sprites[types.Player] == nil {
					tree := pixel.NewSprite(g.spritesheet, g.spriteframes[types.Player][common.DOWN][rand.Intn(len(g.spriteframes[types.Player][common.DOWN]))])
					g.sprites[types.Player] = tree

				}
				g.world.Update(v)
			}
			g.maplock.Unlock()
			if g.Debug {
				log.Println("END UPDATE WORLD")
			}
		case types.Player:

			p := &common.Player{}
			if err := p.Decode(buf[:n]); err != nil {
				log.Fatalln(err)
			}
			log.Println("Got player:", p)
			g.world.Update(p)
			g.spritematrices[p.ID()] = pixel.IM.Scaled(pixel.ZV, 4).Moved(being2vec(p))
		default:
			log.Fatalln("alien packet:", types.Type(t).String())
		}

	}
}

func (g *Game) writeloop() {
	tick := time.Tick(time.Second * 3)

	for range tick {
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
