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
	"runtime"
	"sort"
	"sync/atomic"

	"github.com/aerth/spriteutil"
	"gitlab.com/aerth/x/hash/argon2id"
	"gitlab.com/g4me92bd777b8b16ed4c/common"
	"gitlab.com/g4me92bd777b8b16ed4c/common/chatenc"
	"gitlab.com/g4me92bd777b8b16ed4c/common/chatstack"
	"gitlab.com/g4me92bd777b8b16ed4c/common/codec"
	"gitlab.com/g4me92bd777b8b16ed4c/common/types"

	"golang.org/x/image/colornames"

	"net"

	"time"

	"fmt"
	"net/http"
	_ "net/http/pprof"

	"os"

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/imdraw"
	"github.com/faiface/pixel/pixelgl"
	"github.com/faiface/pixel/text"
	"github.com/pkg/profile"
	basexlib "gitlab.com/aerth/x/encoding/basex"
	"gitlab.com/g4me92bd777b8b16ed4c/assets"
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
func NewGame(playerid uint64) *Game {
	return &Game{
		playerid: playerid,

		// world
		world: worldpkg.New(), // holds position of rectangles

		// sprites
		pictures:       make(map[types.Type]pixel.Picture),
		sprites:        make(map[types.Type]*pixel.Sprite),
		spriteframes:   make(map[types.Type]map[byte][]pixel.Rect),
		spritematrices: make(map[uint64]pixel.Matrix),

		// chat
		chatbuf:   chatstack.New(),
		chatcrypt: chatenc.Load("hello world, 123 451"),

		// switches
		settings: new(RuntimeSettings),
		controls: new(Controls),
		stats:    new(Stats),
	}

}

func (g *Game) Connect() error {
	log.Println("Connecting!")
	// connect to server
	conn, err := common.Connect(g.playerid, DefaultEndpoint, &common.ConnectOptions{})
	if err != nil {
		return err
	}
	log.Println("Connected to:", conn.RemoteAddr())
	conn.SetWriteBuffer(1025)
	conn.SetReadBuffer(1025)
	// cool
	g.conn = conn
	g.codec = codec.NewCodec(common.Endian(), conn)
	return nil

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

	if os.Getenv("DEBUG") != "" {
		defer profile.Start().Stop()
	}

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
		game *Game
		err  error
	)

	// create empty world
	game = NewGame(playerid)
	if err := game.Connect(); err != nil {
		log.Fatalln(err)
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
	// game.spritesheet = spritesheet
	game.sprites[types.Player] = pixel.NewSprite(spritesheet, game.spriteframes[types.Player][common.DOWN][0])
	game.spritematrices[game.playerid] = pixel.IM.Scaled(pixel.ZV, 4)
	game.controls.dpad = new(atomic.Value)
	game.controls.dpad.Store(common.DOWN)

	n, err := game.codec.Write(common.Login{ID: playerid, Password: hashedpassword})
	if err != nil {
		log.Fatalln(err)
	}
	game.stats.netsent += n
	log.Println("sent", n, "bytes for login")
	// killsound := make(chan struct{})
	// nextsound := make(chan struct{})
	//go game.soundtrack(nextsound, killsound)
	go game.readloop()
	go game.writeloop()
	// game.mutechan = killsound
	// game.nextchan = killsound
	game.animations = &animationManager{
		imdraw: imdraw.New(nil),
		//animations: make([]Animation, 10),
		i: 0,
	}

	pixelgl.Run(game.mainloop)
}

type RuntimeSettings struct {
	showChat      bool
	showWireframe bool
	camlock       bool
	Debug         bool
	typing        bool
	SortThings    bool
}

type Stats struct {
	Ping              time.Duration
	netsent, netrecvd int
	displaymsg        byte
	numplayer         int
}

type Controls struct {
	paging     float64
	pageselect byte // shouldnt overflow
	muted      bool
	dpad       *atomic.Value
}
type Game struct {
	settings *RuntimeSettings
	stats    *Stats
	controls *Controls

	// net
	conn  *net.TCPConn
	codec *codec.Codec

	// world
	world *worldpkg.World

	// me
	playerid uint64
	me       worldpkg.Being

	// graphics
	pictures       map[types.Type]pixel.Picture
	sprites        map[types.Type]*pixel.Sprite
	spriteframes   map[types.Type]map[byte][]pixel.Rect
	spritematrices map[uint64]pixel.Matrix

	// chat
	chatbuf   *chatstack.ChatStack // incoming messages
	chattxt   *text.Text           // draw
	chatcrypt *chatenc.Crypt
	win       *pixelgl.Window

	// storage buffer for between frames
	chattxtbuffer bytes.Buffer
	statustxtbuf  bytes.Buffer
	inputbuf      bytes.Buffer // typing

	// ping              int64
	// maplock sync.Mutex

	// mutechan chan struct{}
	// nextchan chan struct{}

	// camera
	camPos       pixel.Vec
	camSpeed     float64
	camZoom      float64
	camZoomSpeed float64
	cam          pixel.Matrix

	animations *animationManager
	//

}

// func (g *Game) soundtrack(next, kill chan struct{}) {

// }

type GhostBomb struct {
	PID    uint64
	pos    pixel.Vec
	aprite *spriteutil.AnimatedSprite
}

func (p GhostBomb) ID() uint64 {
	return p.PID
}

func (p GhostBomb) X() float64 {
	return p.pos.X
}

func (p GhostBomb) Y() float64 {
	return p.pos.Y
}

func (p GhostBomb) Type() types.Type {
	return types.GhostBomb
}
func (p GhostBomb) Pos() pixel.Vec {
	return p.pos
}

func (g *GhostBomb) MoveTo(xy [2]float64) {
	g.pos.X = xy[0]
	g.pos.Y = xy[1]
}

// type Player struct {
// 	sprite *pixel.Sprite
// 	PID    uint64
// 	pos    pixel.Vec
// 	mu     sync.Mutex
// 	HP     float64
// 	MP     float64
// }

// func (e Player) Health() float64 {
// 	return e.HP
// }

// func (e Player) SetHealth(hp float64) {
// 	e.HP = hp
// }
// func (e *Player) DealDamage(from uint64, amount float64) float64 {
// 	if e.HP < amount {
// 		e.HP = 0
// 		return 0
// 	}
// 	e.HP -= amount
// 	if e.HP > 100 {
// 		e.HP = 100
// 	}
// 	return e.HP
// }
// func (p *Player) MoveTo(xy [2]float64) {
// 	p.mu.Lock()
// 	p.pos.X = xy[0]
// 	p.pos.Y = xy[1]
// 	p.mu.Unlock()
// }
// func (p Player) ID() uint64 {
// 	return p.PID
// }

// func (p Player) X() float64 {
// 	p.mu.Lock()
// 	defer p.mu.Unlock()
// 	return p.pos.X
// }

// func (p Player) Y() float64 {
// 	p.mu.Lock()
// 	defer p.mu.Unlock()
// 	return p.pos.Y
// }

// func (p Player) Type() types.Type {
// 	return types.Player
// }
// func (p Player) Pos() pixel.Vec {
// 	return p.pos
// }

var PageAmount = 24.0

const helpModeText = `` +
	`
		WIREFRAME 0, ASYNC 1, DEBUG 4
		CAMLOCK '='
		TOGGLE CHAT 'TAB' CHAT 'Enter"
		COMMAND '/'
		FULLSCREEN 'Ctrl+F'
		Scroll: PGUP/PGDOWN
		Camera: Arrows
		Move: WASD
		Slash: Spacebar
		
`

func mkcolor(i uint64) color.Color {
	return colors[i%uint64(len(colors))]
}

func (g *Game) flashMessage(f string, i ...interface{}) {
	g.stats.displaymsg = 0
	g.statustxtbuf.Reset()
	fmt.Fprintf(&g.statustxtbuf, f, i...)
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
	go func() {
		if err := http.ListenAndServe("localhost:6061", nil); err != nil {
			if err := http.ListenAndServe("localhost:6062", nil); err != nil {
				log.Println("debug listener http:", err)
			}
		}
	}()

	win, err = pixelgl.NewWindow(cfg)
	if err != nil {
		panic(err)
	}

	g.win = win
	g.camPos = pixel.ZV
	g.camSpeed = 500.0
	g.camZoom = 1.0
	g.camZoomSpeed = 1.2

	last := time.Now()

	//pos := pixel.ZV

	newbatch := func(t types.Type, pic pixel.Picture) {
		batches[t] = pixel.NewBatch(&pixel.TrianglesData{}, pic)
		g.pictures[t] = pic
	}

	playerspritesheet := mustLoadPicture("spritesheet/link.png")
	spritesheet2 := mustLoadPicture("spritesheet/overworld_tileset.png")
	heartpic := mustLoadPicture("spritesheet/heart2.png") // 27x23
	uip := mustLoadPicture("spritesheet/overworld_tileset.png")

	// load heart
	heart := pixel.NewSprite(heartpic, heartpic.Bounds())
	var ui *pixel.Batch

	// load ghost
	f, _ := assets.Assets.Open("gif/ghost.gif")
	ghost, err := spriteutil.LoadGif(f)
	if err != nil {
		log.Fatalln(err)
	}
	f.Close()

	newbatch(types.Player, playerspritesheet)
	newbatch(types.TileGrass, spritesheet2)

	//sprites2 := []*pixel.Sprite{}
	//matrix2 := []pixel.Matrix{}
	// spritesheet2frames := map[string]pixel.Rect{
	// 	"X": pixel.R(272, 225, 272+16, 225+16),
	// }
	statustxt := mkfont(0, 18.0, "font/elronmonospace.ttf")
	//statustxt := mkfont(0, 24.0, "font/.ttf")
	second := time.Tick(time.Second)
	fps := 0
	lastdt := 0.0
	lastfps := 0
	lastnetsend := 0
	lastnetrecv := 0
	landframes := map[types.Type]pixel.Rect{
		types.TileGrass: pixel.R(222, 427-170, 222+16, 427-170+16),
		types.TileWater: pixel.R(341, 427-289, 341+16, 427-289+16),
		types.TileRock:  pixel.R(239, 427-153, 239+16, 427-153+16),
	}
	landsheet := spritesheet2
	g.me = g.world.Get(g.playerid)
	if g.me == nil {
		g.world.Update(&common.Player{PID: g.playerid, EntityType: types.Player.Uint16(),
			PosX: 0, PosY: 0, HP: 100, MP: 100})
		g.me = g.world.Get(g.playerid)
	}
	if g.me == nil {
		log.Fatalln("couldnt make main player")
	}
	g.settings.camlock = true
	g.sprites[types.Player] = pixel.NewSprite(g.pictures[types.Player], g.spriteframes[types.Player][common.DOWN][rand.Intn(len(g.spriteframes[types.Player][common.DOWN]))])

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
	gettype := func() types.Type {
		n := types.Type(rand.Intn(5))
		if n >= 3 {
			n = 0
		}
		return n + types.TileGrass
	}
	world := g.world
	wid, height := 16.0, 16.0
	scale := 10.0
	wid *= scale
	height *= scale
	sizegrid := 256.0
	log.Println("Drawing map")
	for y := 0.0; y < height*sizegrid; y = y + height {
		for x := 0.0; x < wid*sizegrid; x = x + wid {
			typ := gettype()
			t := world.NewTile(typ, x, y)
			g.world.SetTile(t)
			g.sprites[t.Type].Draw(batches[t.Type], pixel.IM.Scaled(pixel.ZV, scale).Moved(t.Pos()))
		}
	}
	// for _, t := range themap {
	// 	g.world.SetTile(t)
	// }

	// t := g.world.GetTile(pixel.V(x, y))
	// g.sprites[t.Type].Draw(batches[t.Type], pixel.IM.Moved(t.Pos()))

	uiframes := map[string]pixel.Rect{
		"X":      pixel.R(272, 225, 272+16, 225+16),
		"stone":  pixel.R(86.5, 379, 86.5+16, 379+16),
		"stone2": pixel.R(86.5-16, 379, 86.5, 379+16),
		"blue":   pixel.R(0, 0, 16, 16),
		"grass":  pixel.R(222, 427-169, 222+15, 427-169+15),
	}
	// background := pixel.NewBatch(&pixel.TrianglesData{}, uip)
	// grass := pixel.NewSprite(uip, uiframes["grass"])
	// for xy := 0.0; xy <= win.Bounds().H()+32; xy = xy + 32 {
	// 	for xc := 0.0; xc <= win.Bounds().W()+32; xc = xc + 32 {
	// 		grass.DrawColorMask(background, pixel.IM.Scaled(pixel.ZV, 2).Moved(pixel.V(xc, xy)), colornames.Blueviolet)
	// 	}
	// }

	ui = pixel.NewBatch(&pixel.TrianglesData{}, uip)
	dash := pixel.NewSprite(uip, uiframes["blue"])
	semitr := color.Alpha16{0xaaaa}
	for xc := 0.0; xc <= 2*win.Bounds().W()+16*4; xc = xc + 16*4 {
		dash.DrawColorMask(ui, pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(xc, 0)), semitr)
	}

	//xpos := g.world.Get(g.playerid)
	//pos := pixel.ZV
	fullscreen := false
	clearablebatches := []*pixel.Batch{
		batches[types.Player],
	}
	var numgoroutines = runtime.NumGoroutine()
	var memstats = new(runtime.MemStats)

	fmt.Fprintf(&g.chattxtbuffer, "%s\n\n", helpModeText)
	g.controls.paging += 9 * PageAmount
	playertext := mkfont(0, 12.0, "")
	g.settings.showChat = true
	dt := 0.0
	var worldlen = 0
	fmt.Fprintf(statustxt, "Press ENTER to continue!")

	// for !win.Closed() {
	// 	win.Clear(colornames.Brown)
	// 	if win.JustPressed(pixelgl.KeyEnter) {
	// 		break
	// 	}
	// 	statustxt.Draw(win, pixel.IM.Moved(win.Bounds().Center().Add(pixel.V(-200, 0))))
	// 	win.Update()
	// }
	// win.JustPressed(pixelgl.KeyEnter)
	// win.Pressed(pixelgl.KeyEnter)
	for !win.Closed() {
		dt = time.Since(last).Seconds()
		//playerSpeed = 100.0 * dt
		last = time.Now()
		fps++

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
		if g.settings.typing {
			typin = g.inputbuf.String()
		}
		statustxt.Clear()

		fmt.Fprintf(statustxt, ""+
			"DT=%.0fms FPS=%d PING=%s (%d entities) (%d online) VERSION=%s\n"+
			"NET SENT=%04db/s RECV=%06d b/s GPS=%03.0f,%03.0f TYPING=%q\n%s\n"+
			"heapalloc %v objects %v heap freed %v freed %v sys=%v mallocs=%v\n"+
			"pausetotalNs=%v numgc=%v goroutines=%v\n",
			lastdt*1000, lastfps, g.stats.Ping, worldlen, g.stats.numplayer, Version,
			lastnetsend, lastnetrecv,
			g.me.X(), g.me.Y(),
			typin,
			g.statustxtbuf.String(),
			memstats.HeapAlloc, memstats.HeapObjects, memstats.HeapReleased, memstats.TotalAlloc-memstats.Alloc, memstats.Sys, memstats.Mallocs,
			memstats.PauseTotalNs, memstats.NumGC,
			numgoroutines,
		)

		select {
		default:
		case <-second:
			lastfps = fps
			fps = 0
			lastdt = dt
			lastnetrecv = g.stats.netrecvd
			lastnetsend = g.stats.netsent
			g.stats.netsent = 0
			g.stats.netrecvd = 0
			runtime.ReadMemStats(memstats)
			worldlen = g.world.Len()

			// increment displaymsg and reset buffer if over 5 sec
			g.stats.displaymsg++
			if g.stats.displaymsg > 5 { // show message for 5 seconds
				g.stats.displaymsg = 0
				g.statustxtbuf.Reset()
			}

			numgoroutines = runtime.NumGoroutine()

		}

		cont, err := g.controldpad(dt)
		if err != nil {
			if err.Error() == "quit" {
				log.Println("Bailing!")
				win.Destroy()

				break
			}
			log.Fatalln(err)
		}

		if cont {
			continue
		}
		g.cam = pixel.IM.Scaled(g.camPos, g.camZoom).Moved(win.Bounds().Center().Sub(g.camPos))
		g.camZoom *= math.Pow(g.camZoomSpeed, win.MouseScroll().Y)
		win.Clear(colornames.Forestgreen)
		win.SetMatrix(g.cam)
		//g.win.SetMatrix(pixel.IM.Scaled(g.camPos, 10*g.camZoom).Moved(win.Bounds().Center().Sub(g.camPos)))
		for _, batch := range []*pixel.Batch{batches[types.TileGrass]} {

			batch.Draw(win)
		}
		// begin draw
		//background.Draw(g.win)
		g.win.SetMatrix(g.cam)

		if g.chattxt == nil {
			g.chattxt = mkfont(0, PageAmount, "")
			g.chattxt.Color = colornames.Black
		}

		g.win.SetMatrix(pixel.IM)

		// chat

		g.animations.Update(dt)
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
			g.controls.paging += PageAmount
			fmt.Fprintf(&g.chattxtbuffer, "[%s] (%s) %q\n", from, msg.To, msg.Message)
			fmt.Fprintf(os.Stderr, "[%s] (%s) %q\n", from, msg.To, msg.Message)
		}
		g.chattxt.Clear()
		fmt.Fprintf(g.chattxt, "%s", g.chattxtbuffer.String())
		if g.settings.showChat {
			win.SetMatrix(pixel.IM)
			g.chattxt.DrawColorMask(g.win, pixel.IM.Moved(pixel.V(10, g.win.Bounds().H()-300).Add(pixel.V(0, g.controls.paging))), color.RGBA{0xff, 0xff, 0xff, 0xaa})
		}
		// if g.settings.Debug {
		win.SetMatrix(pixel.IM.Moved(pixel.V(0, g.win.Bounds().Max.Y-32)))
		ui.Draw(g.win) // upper bar
		win.SetMatrix(pixel.IM.Moved(pixel.V(0, g.win.Bounds().Max.Y-64-32)))
		ui.Draw(g.win) // upper bar
		win.SetMatrix(pixel.IM)
		ui.Draw(g.win) // lower bar
		// draw status txt
		statustxt.Draw(win, pixel.IM.Moved(pixel.V(2, g.win.Bounds().Max.Y-PageAmount-1)))
		// }
		win.SetMatrix(g.cam)
		for _, v := range clearablebatches {
			v.Clear()
		}
		g.animations.Clear()
		sorted := g.world.SnapshotBeings()
		_ = mkcolor
		if g.settings.SortThings {
			sort.Sort(sorted)
		}
		//sort.Reverse(sorted)
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
			bpos := being2vec(sorted[i])
			switch sorted[i].Type() {
			case types.GhostBomb:
				ghost.Update(dt)
				ghost.Draw(g.win, pixel.IM.Scaled(pixel.ZV, 1).Moved(being2vec(sorted[i])))
			case types.Player:
				//log.Println("Drawing player sprite")
				dir := byte(rand.Intn(8))
				g.sprites[types.Player].Set(g.pictures[types.Player], g.spriteframes[types.Player][dir][0])
				g.sprites[types.Player].Draw(batches[types.Player], pixel.IM.Scaled(pixel.ZV, 4).Moved(being2vec(sorted[i])))
			default:
				log.Fatalln("WHAT TYPE?!", i, sorted[i].ID(), sorted[i].Type().String())
				//g.world.Remove(sorted[i].ID())
			}
			playertext.Clear()
			fmt.Fprintf(playertext, "%d", sorted[i].ID())
			playertext.Draw(g.win, pixel.IM.Moved(bpos.Add(pixel.V(-10, -60))))
			if sorted[i].Health() != 0 {

				numHearts := math.Floor(sorted[i].Health() / 20)
				for i := 0.0; i < numHearts; i++ {
					heart.Draw(win, pixel.IM.Moved(bpos.Add(pixel.V(-30+(i*16), 48))))
				}
			}
			//	}

		}

		//g.sprites[types.Player].DrawColorMask(batch, g.spritematrices[g.playerid], colornames.Green)
		//g.maplock.Lock()
		// draw player on top
		//	g.spritematrices[g.playerid] = pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(math.Floor(g.pos.X), math.Floor(g.pos.Y)))
		//pos := being2vec(g.me)
		//g.sprites[types.Player].Draw(batches[types.Player], pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(math.Floor(pos.X), math.Floor(pos.Y))))
		//g.sprites[types.Player].Draw(batches[types.Player], pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(math.Floor(g.pos.X), math.Floor(g.pos.Y))))
		//g.maplock.Unlock()
		//g.sprites[types.Player].DrawColorMask(batch, g.spritematrices[g.playerid], colornames.Green)
		//	g.sprites[types.Player].DrawColorMask(batches[types.Player], pixel.IM.Scaled(pixel.ZV, 4).Moved(pixel.V(pos.X, pos.Y)), colornames.White)
		for _, batch := range []*pixel.Batch{batches[types.Player]} {
			//log.Println("Drawing players")
			batch.Draw(win)
		}

		g.animations.Draw(win, being2vec(g.me), g.win.Bounds())
		win.Update()
	}
}

var colors = palette.Plan9

func (g *Game) readloop() {
	buf := make([]byte, 1024)
	var logoff = new(common.PlayerLogoff)
	// var entities = new(worldpkg.Beings)
	var errcount = 0
	for {
		t, reqid, n, err := g.codec.Read(buf)
		if err != nil {
			log.Println(err)
			errcount++
			if errcount > 3 {
				log.Fatalln("max level of errors reached")
			}
		}
		g.stats.netrecvd += n
		if g.settings.Debug {
			log.Printf("Got server packet #%d, type %d (%s) %d bytes", reqid, t, types.Type(t).String(), n)
		}

		switch types.Type(t) {
		case types.Ping:
			ping := &common.Ping{}
			if err := g.codec.Decode(buf[:n], ping); err != nil {
				log.Fatalln(err)
			}
			g.stats.Ping = time.Since(ping.Time)
		case types.Pong:
			ping := &common.Pong{}
			if err := g.codec.Decode(buf[:n], ping); err != nil {
				log.Fatalln(err)
			}
			g.stats.Ping = time.Since(ping.Time)
		case types.UpdateGps:
			log.Printf("UPDATE GPS:     %02x", buf[:n])
		case types.UpdatePlayers:
			log.Printf("UPDATE PLAYERS: %02x", buf[:n])
		case types.RemoveEntity:

			m := &common.HealthReport{}
			if err := g.codec.Decode(buf[:n], &m); err != nil {
				log.Fatalln("health report", err)
			}
			log.Println("committing health report:", m)
			for playerid, v := range m.M {

				if g.world.DealDamage(v.From, v.ID, v.Dam) != v.Cur {
					log.Println("damage mismatch occured")
				}
				if v.Cur == 0 {
					g.flashMessage("Player %d died!", playerid)
					g.world.Remove(playerid)
				}
			}
		case types.PlayerMessage:

			m := &common.PlayerMessage{}
			if err := g.codec.Decode(buf[:n], m); err != nil {
				log.Fatalln(err)
			}
			go func() {
				b, err := basex.Decode(m.Message)
				if err == nil {
					msg := g.chatcrypt.Decrypt(b)
					if msg == nil {
						log.Println("COULD NOT DECRYPT")
						return
					}
					log.Println("Got message:", m.Message)
					g.chatbuf.Push(chatstack.ChatMessage{
						From:    fmt.Sprintf("%d", m.From),
						To:      fmt.Sprintf("%d", m.To),
						Message: string(msg),
					})
				}
			}()
		case types.PlayerLogoff:
			if err := g.codec.Decode(buf[:n], logoff); err != nil {
				log.Fatalln(err)
			}
			if logoff.UID == g.playerid {
				log.Println("YOU DIEDE")
				g.flashMessage("You died. Press any key to respawn.")
				g.me.MoveTo([2]float64{0, 0})
				g.world.Update(g.me)
				continue
			}
			go func() {
				log.Println("LOGGING OFF:", logoff.UID)
				// delete(g.spritematrices, logoff.UID)
				g.world.Remove(logoff.UID)
				g.flashMessage("Player logged off: %d", logoff.UID)
				// g.stats.numplayer--
			}()

		case types.PlayerAction:
			//	g.maplock.Lock()
			a := common.PlayerAction{}
			if err := g.codec.Decode(buf[:n], &a); err != nil {
				log.Fatalln(err)
			}

			being := g.world.Get(a.ID)
			if being == nil {
				// switch a.Type() {
				// case types.Human:
				// 	being = &Player{PID: a.ID}
				// case types.GhostBomb:
				// 	being = &GhostBomb{PID: a.ID}
				// default:
				log.Println("Skipping player action from dead/unknown entity:", a.Type().String())
				continue
			}

			oldv := pixel.V(float64(being.X()), float64(being.Y()))
			if g.settings.Debug {
				log.Println("got player action from server:", a.ID, "NEWPOS", a.Pos(), common.DPAD(a.DPad), "OLD POS", oldv, "HP:", a.HP)
			}

			// update world with new being position
			pv := pixel.V(float64(a.At[0]), float64(a.At[1]))
			being.MoveTo([2]float64{pv.X, pv.Y})
			being.SetHealth(a.HP)
			if being.Health() != a.HP {
				log.Fatalf("Couldnt set being health: %T, %02.0f, %02.0f", being, a.HP, being.Health())
			}
			g.world.Update(being)
			if being.Health() == 0 {
				//g.world.Remove(being.ID())
				//log.Println("removed dead thing")
				g.flashMessage("removed dead player: %d", a.ID)
				continue
			}
			//log.Printf("%s %d has HP %02.0f", being.Type(), a.ID, being.Health(), a.HP)

			if a.Action != 0 {
				if a.ID != g.playerid {
					g.animations.Push(types.ActionManastorm, pv)
					g.flashMessage("Player %d cast %s", a.ID, types.Type(a.Action).String())
				}
			}

			// case types.World:
		// if g.settings.Debug {
		// 	log.Printf("UPDATE WORLD:   %02x", buf[:n])
		// }
		// if err := entities.Decode(buf[:n]); err != nil {
		// 	log.Fatalln("decode world:", err)
		// }
		// go func() {
		// 	if g.settings.Debug {
		// 		log.Printf("GOT WORLD, len %d, bytes %02x", len(*entities), buf[:n])
		// 	}
		// 	if len(*entities) == 0 {
		// 		log.Fatalln("world is gone")
		// 	}

		// 	for i, v := range *entities {
		// 		if g.settings.Debug {
		// 			log.Printf("NEWMAP: [%d] %d (%02.2f,%02.2f) T=%d (%s) %T", i, v.ID(), v.X(), v.Y(), v.Type(), common.TYPE(v.Type()), v)
		// 		}
		// 		g.world.Update(v)
		// 	}

		// 	if g.settings.Debug {
		// 		log.Println("END UPDATE WORLD")
		// 	}

		case types.Player:

			p := &common.Player{}
			if err := g.codec.Decode(buf[:n], p); err != nil {
				log.Fatalln(err)
			}
			if p.HP == 0 {
				log.Fatalln("got 0 player")
			}
			go func() {
				//log.Println("Got player:", p)
				if newplayer := g.world.Update(p); newplayer {
					g.stats.numplayer++
				}
				//g.spritematrices[p.ID()] = pixel.IM.Scaled(pixel.ZV, 4).Moved(being2vec(p))
			}()
		case types.GhostBomb:

			p := &common.Player{}
			if err := g.codec.Decode(buf[:n], p); err != nil {
				log.Fatalln(err)
			}
			if p.HP == 0 {
				log.Fatalln("got 0 ghostbomb")
			}
			go func() {
				//log.Println("Got player:", p)
				g.world.Update(p)
				//g.spritematrices[p.ID()] = pixel.IM.Scaled(pixel.ZV, 4).Moved(being2vec(p))
			}()
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
		g.stats.netsent += n
	}

}

func being2vec(b worldpkg.Being) pixel.Vec {
	return pixel.V(b.X(), b.Y())
}

// func mkcolortransparent(c color.Color, opacity float64) (out color.RGBA) {
// 	out.A = uint8(math.Floor(opacity * 255.0))
// 	r, g, b, _ := c.RGBA()
// 	out.R, out.G, out.B = uint8(r>>4), uint8(g>>4), uint8(b>>4)
// 	return
// }
