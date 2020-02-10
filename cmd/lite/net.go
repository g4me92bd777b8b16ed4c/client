package main

import (
	_ "image/png"
	"log"

	"gitlab.com/g4me92bd777b8b16ed4c/common"
	// "gitlab.com/g4me92bd777b8b16ed4c/common/chatstack"
	"gitlab.com/g4me92bd777b8b16ed4c/common/types"

	"time"

	"fmt"
	_ "net/http/pprof"

	"github.com/faiface/pixel"
)
type ChatMessage struct {
	From    string `json:'from'`         // empty = from world/region
	To      string `json:'to',omitempty` // empty = world/region
	Message string `json:'msg'`
}
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
			continue
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
				if err != nil {
					log.Println("?", string(b))
					return
				}
					msgb := g.chatcrypt.Decrypt(b)
					if msgb == nil {
						log.Println("?", m.Message)
						return
					}
					msg := ChatMessage{
						From:    fmt.Sprintf("%d", m.From),
						To:      fmt.Sprintf("%d", m.To),
						Message: string(g.chatcrypt.Decrypt(b)),
					}

						from := msg.From
						if len(from) > 5 {
							from = from[:5]
						}
						g.controls.paging += PageAmount
						fmt.Fprintf(&g.chattxtbuffer, "[%s] (%s) %q\n", from, msg.To, msg.Message)
						log.Printf("[%s] (%s) %q\n", from, msg.To, msg.Message)
						
				
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
			go func() {
				//log.Println("Got player:", p)
				g.world.Update(p)
				//g.spritematrices[p.ID()] = pixel.IM.Scaled(pixel.ZV, 4).Moved(being2vec(p))
			}()
		default:
			errcount++
			log.Println("alien packet:", types.Type(t).String())
		}

	}
}

func (g *Game) writeloop() {
	tick := time.Tick(time.Second * 3)
	for {
		select {
		case <-tick:
			n, err := g.codec.Write(common.Ping{ID: g.playerid, Time: time.Now().UTC()})
			if err != nil {
				log.Fatalln("write ping error:", err)
			}
			g.stats.netsent += n
		case x := <-g.sendChan:
			n, err := g.codec.Write(x)
			if err != nil {
				log.Fatalln("write msg error:", err)
			}
			g.stats.netsent += n
		}

	}
}
