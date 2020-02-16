package main

import (
	"time"

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/imdraw"

	//"github.com/g4me92bd777b8b16ed4c/common"
	"log"

	"github.com/g4me92bd777b8b16ed4c/common/types"
)

type Animation interface {
	Update(dt float64)
	Draw(target pixel.Target, matrix pixel.Matrix)
	Until() time.Time
	Type() types.Type
}
type animationManager struct {
	imdraw     *imdraw.IMDraw
	animations []Animation
	i          int // how many animations are being managed
}
type effect1 struct {
	At     pixel.Vec
	until  time.Time
	radius float64
	imdraw *imdraw.IMDraw
	//dead   bool
	i int
}

func (e effect1) Type() types.Type {
	return types.ActionManastorm
}
func (e effect1) Until() time.Time {

	return e.until
}
func (e *effect1) Draw(t pixel.Target, m pixel.Matrix) {
	e.imdraw.Color = colors[e.i%len(colors)]
	e.imdraw.Push(e.At)
	e.imdraw.Circle(e.radius, 2.0)
}

func (e *effect1) Update(dt float64) {
	if e == nil {
		return
	}
	max := 200.0
	amount := 23.0

	e.radius += amount * dt
	if e.radius > max {
		e.radius = 0
	}
	e.i++
	//	log.Println("Updating:", dt, e.radius)
}

func (a *animationManager) Push(t types.Type, at pixel.Vec) {
	if a.i+1 > len(a.animations) {
		log.Println("allocatitng one more.. for push animation")
		a.animations = append(a.animations, nil)
	}
	a.animations[a.i] = &effect1{
		At:     at,
		radius: 10.0,
		imdraw: a.imdraw,
		until:  time.Now().Add(time.Second * 3),
	}
	log.Println("Pushed Animation:", a.i, a.animations[a.i], time.Until(a.animations[a.i].Until()))
	a.i++
	log.Println("Next push will be:", a.i)

}

func (a *animationManager) Update(dt float64) {
	cleanAnimations(&a.animations, &a.i)

	for i := 0; i < a.i; i++ {
		if a.animations[i] != nil {
			a.animations[i].Update(dt)
		}
	}
}
func cleanAnimations(a *[]Animation, n *int) {
	if *n == 0 {
		return
	}

	var x = *n - 1
	var max = len(*a)
	if max-1 < x {
		x = max - 1
	}

	for ; x >= 0; x-- {
		if (*a)[x].Until().Before(time.Now()) {
			log.Println("Expiring animation:", ((*a)[x]).Type().String())
			*n--
			if x == max {
				*a = (*a)[:x]
			} else {
				*a = append((*a)[:x], (*a)[x+1:]...)
			}
		}
	}
}
func (a animationManager) Clear() {
	a.imdraw.Reset()
	a.imdraw.Clear()
}
func (a animationManager) Draw(target pixel.Target, pos pixel.Vec, bounds pixel.Rect) {
	//	a.animations = cleanAnimations(a.animations)
	for i := 0; i < a.i; i++ {
		a.animations[i].Draw(target, pixel.IM.Moved(pos))
	}
	a.imdraw.Draw(target)
}
