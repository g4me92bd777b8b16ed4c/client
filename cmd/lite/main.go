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
	"strings"
	"sync/atomic"

	"github.com/aerth/spriteutil"
	"github.com/bcvery1/tilepix"
	"gitlab.com/aerth/x/hash/argon2id"
	"github.com/g4me92bd777b8b16ed4c/common"
	"github.com/g4me92bd777b8b16ed4c/common/chatenc"
	// "github.com/g4me92bd777b8b16ed4c/common/chatstack"
	"github.com/g4me92bd777b8b16ed4c/common/codec"
	"github.com/g4me92bd777b8b16ed4c/common/plugint"
	"github.com/g4me92bd777b8b16ed4c/common/types"
	"github.com/g4me92bd777b8b16ed4c/common/world"
	"golang.org/x/image/colornames"

	"net"

	"time"

	"fmt"
	_ "net/http/pprof"

	"os"

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/imdraw"
	"github.com/faiface/pixel/pixelgl"
	"github.com/faiface/pixel/text"
	"github.com/pkg/profile"
	basexlib "gitlab.com/aerth/x/encoding/basex"
	"github.com/g4me92bd777b8b16ed4c/assets"
	worldpkg "github.com/g4me92bd777b8b16ed4c/common/world"
)

var (
	Version            string
	DefaultEndpoint    = ""
	defaultChatChannel = "1" // hashed into private key for e2e group chat .. lol
) // set by makefile / ldflags

var basex = basexlib.NewAlphabet(basexlib.Base58Bitcoin)
var colors = palette.Plan9

func being2vec(b world.Being) pixel.Vec {
	return pixel.V(b.X(), b.Y())
}

// func init() {
// 	worldpkg.Type = types.World.Byte()
// }
func NewGame(playerid uint64) *Game {
	return &Game{
		playerid: playerid,
		// world
		world: worldpkg.New(), // holds position of rectangles


		// switches
		settings: DefaultSettings(),
		controls: new(Controls),
		stats:    new(Stats),

		sendChan: make(chan types.Typer),

		// sprites for beings
		sprites: make(map[types.Type]interface {
			Draw(pixel.Target, pixel.Matrix)
		}),
		chatcrypt: chatenc.Load("hello world, 123 451"),
	}
}

func DefaultSettings() *RuntimeSettings {
	return &RuntimeSettings{
		keymap: DefaultKeymap(),
		showChat: true,
		showWireframe: true,
		showLand: true,
		showPlayerText: true,
		showEntities: true,
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

// TODO: animation... lol no point yet
var playerframe = pixel.R(0, 360, 20, 390)

type UpdateFunc func(*worldpkg.World, *uint8, *codec.Codec)

func main() {
	// read command line flags

	log.SetFlags(log.Lshortfile | log.Lmicroseconds)
	// NOTE FROM THE AUTHOR: NOT SECURE CRYPTO, JUST RAND FOR GAMES
	now := time.Now().UTC().UnixNano()
	rand.Seed(now) // random enough seed
	playerid := rand.Uint64()
	password := ""
	plugins := ""
	flag.StringVar(&DefaultEndpoint, "s", DefaultEndpoint, "endpoint")
	flag.Uint64Var(&playerid, "p", playerid, "endpoint")
	flag.StringVar(&defaultChatChannel, "chatcrypt", "", "chatcrypt seed for secure messaging")
	flag.StringVar(&password, "pass", "", "endpoint")
	flag.StringVar(&plugins, "plugins", "", "comma (no spaces) separated plugins to load at launch (use /loadplugin path/to/dot.so in-game)")
	flag.Parse()
	rand.Seed(time.Now().UTC().UnixNano()) // random enough seed
	if playerid == 0 {
		playerid = rand.Uint64()
	}
	log.Printf("Helo player %d", playerid)
	var hashedpassword [32]byte
	copy(hashedpassword[:], argon2id.New(1, 24, 1).Sum([]byte(password)))
	password = ""

	var (
		game *Game
		err  error
	)

	if os.Getenv("PROFILE") != "" {
		defer profile.Start().Stop()
	}

	// create empty world
	game = NewGame(playerid)
	if err := game.Connect(); err != nil {
		log.Fatalln(err)
	}
	game.plugins = strings.Split(plugins, ",")
	if len(game.plugins) == 1 && game.plugins[0] == "" { 
		game.plugins = nil
	}
	game.controls.dpad = new(atomic.Value)
	game.controls.dpad.Store(common.DOWN)

	// login to server
	n, err := game.codec.Write(common.Login{ID: playerid, Password: hashedpassword})
	if err != nil {
		log.Fatalln(err)
	}
	game.stats.netsent += n
	log.Println("sent", n, "bytes for login")

	go game.readloop()
	go game.writeloop()

	game.animations = &animationManager{
		imdraw: imdraw.New(nil),
		i:      0,
	}

	pixelgl.Run(game.mainloop)
}

type RuntimeSettings struct {
	showChat      bool
	showWireframe bool
	showLand bool
	showPlayerText bool
	showEntities bool
	camlock       bool
	Debug         bool
	typing        bool
	SortThings    bool
	
	keymap        keymap
	mousedisabled bool
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

	// chat
	// chatbuf   *chatstack.ChatStack // incoming messages
	chattxt   *text.Text           // draw
	chatcrypt *chatenc.Crypt
	win       *pixelgl.Window

	// storage buffer for between frames
	chattxtbuffer bytes.Buffer
	statustxtbuf  bytes.Buffer
	inputbuf      bytes.Buffer // typing
	cam           Camera
	animations    *animationManager
	sendChan      chan types.Typer
	pluginCanvas  *pixel.Batch
	updateFns     []plugint.PluginUpdateFunc
	drawFns       []func(pixel.Target)
	plugins       []string // path to plugins

	canvas *pixelgl.Canvas
	// each being type has a sprite, initialized at boot, or in loading sequence
	sprites map[types.Type]interface {
		Draw(pixel.Target, pixel.Matrix)
	}
	//

}

type Camera struct {
	// camera
	camPos       pixel.Vec
	camSpeed     float64
	camZoom      float64
	camZoomSpeed float64
	cam          pixel.Matrix
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

const PageAmount = 16.0

const helpModeText = `` +
	`
[help]
	WIREFRAME 0, ASYNC 1, DEBUG 4
	CAMLOCK '='
	TOGGLE CHAT 'TAB' CHAT 'Enter"
	COMMAND '/'
	FULLSCREEN 'Ctrl+F'
	Scroll: PGUP/PGDOWN
	Camera: Arrows
	Move: WASD
	Slash: Spacebar

Welcome to the world, player %d.
Press PageDown to hide this chat

`

var PlayerViewport = pixel.R(-500, -500, 500, 500)

func mkcolor(i uint64) color.Color {
	return colors[i%uint64(len(colors))]
}
func (g *Game) loadPlugin(pluginPath string) error {
	updateFn, drawFn, err := plugint.RegisterPlugin(g.world, g.codec, g.pluginCanvas, pluginPath)
	if err != nil {
		return err
	}

	// plugins can update and move dpad
	if updateFn != nil {
		g.updateFns = append(g.updateFns, updateFn)
	}

	// plugins can draw
	if drawFn != nil {
		g.drawFns = append(g.drawFns, drawFn)
	}
	return nil
}

func (g *Game) flashMessage(f string, i ...interface{}) {
	g.stats.displaymsg = 0
	g.statustxtbuf.Reset()
	fmt.Fprintf(&g.statustxtbuf, f, i...)
}
func (g *Game) flashMessageToChat(f string, i ...interface{}) {
	fmt.Fprintf(&g.chattxtbuffer, f, i...)
}
func (g *Game) mainloop() {
	var (
		err error
	)
	g.win, err = pixelgl.NewWindow(pixelgl.WindowConfig{
		Title:     "Game Title",
		Bounds:    pixel.R(0, 0, 700, 500),
		VSync:     true,
		Resizable: true,
	})
	if err != nil {
		log.Fatalln(err)
	}

	g.cam.camPos = pixel.ZV
	g.cam.camSpeed = 500.0
	g.cam.camZoom = 1.0
	g.cam.camZoomSpeed = 1.2

	playerspritesheet := mustLoadPicture("spritesheet/link.png")
	heartpic := mustLoadPicture("spritesheet/heart2.png") // 27x23

	// load heart
	heart := pixel.NewSprite(heartpic, heartpic.Bounds())

	// load ghost
	f, _ := assets.Assets.Open("gif/ghost.gif")
	ghost, err := spriteutil.LoadGif(f)
	if err != nil {
		log.Fatalln(err)
	}
	f.Close()

	playersprite := pixel.NewSprite(playerspritesheet, playerframe)

	// register drawable sprites
	g.sprites[types.Player] = playersprite
	g.sprites[types.GhostBomb] = ghost

	statustxt := mkfont(0, 12.0, "font/firacode.ttf")
	playertext := mkfont(0, 12.0, "font/square.ttf")
	g.chattxt = mkfont(0, PageAmount, "font/square.ttf")
	g.chattxt.Color = colornames.Black
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

	wid, height := 16.0, 16.0
	scale := 10.0
	wid *= scale
	
	height *= scale
	f, err = assets.Assets.Open("maps/landingpad.tmx")
	if err != nil {
		log.Fatalln(err)
	}
	mooo, err := tilepix.Read(f, "maps", assets.Assets.Open)
	if err != nil {
		f.Close()
		panic(err)
	}
	f.Close()

	fullscreen := false

	var numgoroutines = runtime.NumGoroutine()
	var memstats = new(runtime.MemStats)

	fmt.Fprintf(&g.chattxtbuffer, helpModeText, g.playerid)
	g.controls.paging += 15 * PageAmount

	g.settings.showChat = true
	dt := 0.0
	var worldlen = new(atomic.Value) // updates every second (or on new entity?)
	worldlen.Store(g.world.Len())    // always at least 1 (the player)

	// Load Plugins (fills g.updateFns and g.drawFns)
	g.pluginCanvas = pixel.NewBatch(&pixel.TrianglesData{}, nil)
	for _, pluginPath := range g.plugins {
		if err := g.loadPlugin(pluginPath); err != nil {
			log.Printf("error loading plugin %q: %v", pluginPath, err)
			<-time.After(time.Second)
		}
	}

	second := time.Tick(time.Second)
	fps := 0
	lastdt := 0.0
	lastfps := 0
	lastnetsend := 0
	lastnetrecv := 0
	last := time.Now()
	g.canvas = pixelgl.NewCanvas(g.win.Bounds())
	for !g.win.Closed() {
		dt = time.Since(last).Seconds()
		//playerSpeed = 100.0 * dt
		last = time.Now()
		fps++
		g.canvas.SetBounds(g.win.Bounds())
		// dpad = 0

		// dir.X = 0
		// dir.Y = 0
		g.canvas.SetMatrix(g.cam.cam)
		g.me = g.world.Get(g.playerid)

		if g.win.JustPressed(pixelgl.KeyF) && g.win.Pressed(pixelgl.KeyLeftControl) {
			fullscreen = !fullscreen
			if fullscreen {
				g.win.SetMonitor(pixelgl.PrimaryMonitor())
			} else {
				g.win.SetMonitor(nil)
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
			"pausetotalNs=%v numgc=%v goroutines=%v\n"+
			"HP: %02.0f\nMP: %02.0f\nXP: %02.0f\n",
			lastdt*1000, lastfps, g.stats.Ping, worldlen.Load().(int), g.stats.numplayer, Version,
			lastnetsend, lastnetrecv,
			g.me.X(), g.me.Y(),
			typin,
			g.statustxtbuf.String(),
			memstats.HeapAlloc, memstats.HeapObjects, memstats.HeapReleased, memstats.TotalAlloc-memstats.Alloc, memstats.Sys, memstats.Mallocs,
			memstats.PauseTotalNs, memstats.NumGC,
			numgoroutines,
			g.me.Health(), 0.0, 0.0,
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
			worldlen.Store(g.world.Len())

			// increment displaymsg and reset buffer if over 5 sec
			g.stats.displaymsg++
			if g.stats.displaymsg > 5 { // show message for 5 seconds
				g.stats.displaymsg = 0
				g.statustxtbuf.Reset()
			}

			numgoroutines = runtime.NumGoroutine()

		}
		runtime.Gosched()
		cont, err := g.controldpad(dt)
		if err != nil {
			if err.Error() == "quit" {
				log.Println("Bailing!")
				g.win.Destroy()
				break
			}
			log.Fatalln(err)
		}
		if cont {
			continue
		}
		runtime.Gosched()
		g.cam.cam = pixel.IM.Scaled(g.cam.camPos, g.cam.camZoom).Moved(g.canvas.Bounds().Center().Sub(g.cam.camPos))

		g.cam.camZoom = pixel.Clamp(g.cam.camZoom*math.Pow(g.cam.camZoomSpeed, g.win.MouseScroll().Y), 0.5, 2.5)
		g.canvas.Clear(colornames.Forestgreen)
		g.chattxt.Clear()
		g.animations.Clear()
		g.pluginCanvas.Clear()

		g.canvas.SetMatrix(pixel.IM.Scaled(g.cam.camPos, g.cam.camZoom).Moved(g.canvas.Bounds().Center().Sub(g.cam.camPos)))
		if g.settings.showLand {
			for _, l := range mooo.TileLayers {
				if err := l.Draw(g.canvas); err != nil {	
					log.Fatalln(err)
				}
			}
		} else {
			for _, il := range mooo.ImageLayers {
				// The matrix shift is because images are drawn from the top-left in Tiled.
				if err := il.Draw(g.canvas, pixel.IM.Moved(pixel.V(0, float64(mooo.Height * mooo.TileHeight)))); err != nil {	
					log.Fatalln(err)
				}
			}
		}
		
		// if err := mooo.DrawAll(g.canvas, color.Transparent, pixel.IM); err != nil {
		// 	log.Fatalln(err)
		// }
		

		g.animations.Update(dt)

		// var msg chatstack.ChatMessage
		// for i := 0; i < 10; i++ {
		// 	msg = g.chatbuf.Pop()
		// 	if msg.Message == "" {
		// 		break
		// 	}
		// 	log.Println("Popped chat messages:", i+1)
		// 	from := msg.From
		// 	if len(from) > 5 {
		// 		from = from[:5]
		// 	}
		// 	g.controls.paging += PageAmount
		// 	fmt.Fprintf(&g.chattxtbuffer, "[%s] (%s) %q\n", from, msg.To, msg.Message)
		// 	fmt.Fprintf(os.Stderr, "[%s] (%s) %q\n", from, msg.To, msg.Message)
		// }

		fmt.Fprintf(g.chattxt, "%s", g.chattxtbuffer.String())

		if g.settings.showChat {
			g.canvas.SetMatrix(pixel.IM)
			g.chattxt.Draw(g.canvas, pixel.IM.Moved(pixel.V(10, g.canvas.Bounds().H()-300).Add(pixel.V(0, g.controls.paging))))
		}

		g.canvas.SetMatrix(pixel.IM)

		statustxt.Draw(g.canvas, pixel.IM.Moved(pixel.V(2, g.canvas.Bounds().Max.Y-PageAmount-1)))
		// }
		// if g.settings.Debug {
		// 	g.win.Update()
		// 	continue
		// }
		//g.canvas.SetMatrix(g.cam.cam)

		g.canvas.SetMatrix(pixel.IM.Scaled(g.cam.camPos, g.cam.camZoom).Moved(g.canvas.Bounds().Center().Sub(g.cam.camPos)))
		// for _, v := range clearablebatches {
		// 	v.Clear()
		// }

		sorted := g.world.SnapshotBeings()
		_ = mkcolor
		if g.settings.SortThings {
			sort.Sort(sorted)
		}
		playerArea := PlayerViewport.Moved(being2vec(g.me))
		//sort.Reverse(sorted)

		// draw nearby players and entities
		for i := range sorted {
			
			var bpos pixel.Vec = being2vec(sorted[i])
			if playerArea.Contains(bpos) {
				if g.settings.showEntities {
				g.drawBeing(sorted[i])
				}
				if g.settings.showPlayerText {
					hp := sorted[i].Health()
				playertext.Clear()
				fmt.Fprintf(playertext, "%d (HP=%02.0f)", sorted[i].ID(), hp)
				playertext.Draw(g.canvas, pixel.IM.Moved(bpos.Add(pixel.V(-20, -30))))
				
				if hp != 0 {
					numHearts := math.Floor(hp / 20)
					offset := -numHearts / 2 * numHearts
					for i := 0.0; i < numHearts; i++ {
						heart.Draw(g.canvas, pixel.IM.Scaled(pixel.ZV, 0.3).Moved(bpos.Add(pixel.V(offset+(i*7), 18))))
					}
				}
			}
			}
			
		}
		// draw
		for _, pluginDrawer := range g.drawFns {
			 pluginDrawer(g.pluginCanvas)
		}
		g.animations.Draw(g.canvas, being2vec(g.me), g.canvas.Bounds())
		g.pluginCanvas.Draw(g.canvas)
		g.canvas.Draw(g.win, pixel.IM.Moved(g.win.Bounds().Center()))
		g.win.Update()
	}
}

func (g *Game) drawBeing(t worldpkg.Being) {
	var s interface {
		Draw(pixel.Target, pixel.Matrix)
	}
	var ok bool
	s, ok = g.sprites[t.Type()]
	if !ok {
		log.Println("no sprite for being type: %s (%d)", t.Type().String(), t.Type())
	}
	s.Draw(g.canvas, pixel.IM.Moved(being2vec(t)))
}
