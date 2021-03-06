package main

import (
	"encoding/csv"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	_ "image/png"

	"github.com/faiface/pixel"
	"github.com/faiface/pixel/imdraw"
	"github.com/faiface/pixel/pixelgl"
	"github.com/pkg/errors"
)

var level int
var bgcolour pixel.RGBA

func loadAnimationSheet(sheetPath, descPath string, frameWidth float64) (sheet pixel.Picture, anims map[string][]pixel.Rect, err error) {
	// total hack, nicely format the error at the end, so I don't have to type it every time
	defer func() {
		if err != nil {
			err = errors.Wrap(err, "error loading animation sheet")
		}
	}()

	// open and load the spritesheet
	sheetFile, err := os.Open(sheetPath)
	if err != nil {
		return nil, nil, err
	}
	defer sheetFile.Close()
	sheetImg, _, err := image.Decode(sheetFile)
	if err != nil {
		return nil, nil, err
	}
	sheet = pixel.PictureDataFromImage(sheetImg)

	// create a slice of frames inside the spritesheet
	var frames []pixel.Rect
	for x := 0.0; x+frameWidth <= sheet.Bounds().Max.X; x += frameWidth {
		frames = append(frames, pixel.R(
			x,
			0,
			x+frameWidth,
			sheet.Bounds().H(),
		))
	}

	descFile, err := os.Open(descPath)
	if err != nil {
		return nil, nil, err
	}
	defer descFile.Close()

	anims = make(map[string][]pixel.Rect)

	// load the animation information, name and interval inside the spritesheet
	desc := csv.NewReader(descFile)
	for {
		anim, err := desc.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}

		name := anim[0]
		start, _ := strconv.Atoi(anim[1])
		end, _ := strconv.Atoi(anim[2])

		anims[name] = frames[start : end+1]
	}

	return sheet, anims, nil
}

type platform struct {
	rect  pixel.Rect
	color color.Color
}

func (p *platform) draw(imd *imdraw.IMDraw) {
	imd.Color = p.color
	imd.Push(p.rect.Min, p.rect.Max)
	imd.Rectangle(0)
}

type gopherPhys struct {
	gravity   float64
	runSpeed  float64
	jumpSpeed float64

	rect   pixel.Rect
	vel    pixel.Vec
	ground bool
}

func (gp *gopherPhys) update(dt float64, ctrl pixel.Vec, platforms0 []platform, phys *gopherPhys, goal *goal) {
	// apply controls
	switch {
	case ctrl.X < 0:
		gp.vel.X = -gp.runSpeed
	case ctrl.X > 0:
		gp.vel.X = +gp.runSpeed
	default:
		gp.vel.X = 0
	}

	// apply gravity and velocity
	gp.vel.Y += gp.gravity * dt
	gp.rect = gp.rect.Moved(gp.vel.Scaled(dt))

	// check collisions against each platform
	gp.ground = false
	if gp.vel.Y <= 0 {
		for _, p := range platforms0 {
			if gp.rect.Max.X <= p.rect.Min.X || gp.rect.Min.X >= p.rect.Max.X {
				continue
			}
			if gp.rect.Min.Y > p.rect.Max.Y || gp.rect.Min.Y < p.rect.Max.Y+gp.vel.Y*dt {
				continue
			}
			gp.vel.Y = 0
			gp.rect = gp.rect.Moved(pixel.V(0, p.rect.Max.Y-gp.rect.Min.Y))
			gp.ground = true
		}
	}

	// jump if on the ground and the player wants to jump
	if gp.ground && ctrl.Y > 0 {
		gp.vel.Y = gp.jumpSpeed
	}

	// die if below -300
	if gp.rect.Min.Y < -300.0 {
		phys.rect = phys.rect.Moved(phys.rect.Center().Scaled(-1))
		phys.vel = pixel.ZV
	}

	fmt.Print(gp.rect.Min.X)
	fmt.Print(" | ")
	fmt.Println(gp.rect.Min.Y)

	// check if the player is at the goal so therefor should update the lvl
	if gp.rect.Min.X >= (goal.pos.X-(goal.radius+10)) && gp.rect.Min.X <= (goal.pos.X+(goal.radius)) {
		if gp.rect.Min.Y >= (goal.pos.Y-(goal.radius+10)) && gp.rect.Min.Y <= (goal.pos.Y+(goal.radius)) {
			if level != 2 {
				level++ // up lvl counter

				// change background
				bgcolour = randomNiceBGColor()

				// go to middle of map again
				phys.rect = phys.rect.Moved(phys.rect.Center().Scaled(-1))
				phys.vel = pixel.ZV
			}
		}
	}
}

type animState int

const (
	idle animState = iota
	running
	jumping
)

type gopherAnim struct {
	sheet pixel.Picture
	anims map[string][]pixel.Rect
	rate  float64

	state   animState
	counter float64
	dir     float64

	frame pixel.Rect

	sprite *pixel.Sprite
}

func (ga *gopherAnim) update(dt float64, phys *gopherPhys) {
	ga.counter += dt

	// determine the new animation state
	var newState animState
	switch {
	case !phys.ground:
		newState = jumping
	case phys.vel.Len() == 0:
		newState = idle
	case phys.vel.Len() > 0:
		newState = running
	}

	// reset the time counter if the state changed
	if ga.state != newState {
		ga.state = newState
		ga.counter = 0
	}

	// determine the correct animation frame
	switch ga.state {
	case idle:
		ga.frame = ga.anims["Front"][0]
	case running:
		i := int(math.Floor(ga.counter / ga.rate))
		ga.frame = ga.anims["Run"][i%len(ga.anims["Run"])]
	case jumping:
		speed := phys.vel.Y
		i := int((-speed/phys.jumpSpeed + 1) / 2 * float64(len(ga.anims["Jump"])))
		if i < 0 {
			i = 0
		}
		if i >= len(ga.anims["Jump"]) {
			i = len(ga.anims["Jump"]) - 1
		}
		ga.frame = ga.anims["Jump"][i]
	}

	// set the facing direction of the gopher
	if phys.vel.X != 0 {
		if phys.vel.X > 0 {
			ga.dir = +1
		} else {
			ga.dir = -1
		}
	}
}

func (ga *gopherAnim) draw(t pixel.Target, phys *gopherPhys) {
	if ga.sprite == nil {
		ga.sprite = pixel.NewSprite(nil, pixel.Rect{})
	}
	// draw the correct frame with the correct position and direction
	ga.sprite.Set(ga.sheet, ga.frame)
	ga.sprite.Draw(t, pixel.IM.
		ScaledXY(pixel.ZV, pixel.V(
			phys.rect.W()/ga.sprite.Frame().W(),
			phys.rect.H()/ga.sprite.Frame().H(),
		)).
		ScaledXY(pixel.ZV, pixel.V(-ga.dir, 1)).
		Moved(phys.rect.Center()),
	)
}

type goal struct {
	pos    pixel.Vec
	radius float64
	step   float64

	counter float64
	cols    [5]pixel.RGBA
}

func (g *goal) update(dt float64) {
	g.counter += dt
	for g.counter > g.step {
		g.counter -= g.step
		for i := len(g.cols) - 2; i >= 0; i-- {
			g.cols[i+1] = g.cols[i]
		}
		g.cols[0] = randomNiceColor()
	}
}

func (g *goal) draw(imd *imdraw.IMDraw) {
	for i := len(g.cols) - 1; i >= 0; i-- {
		imd.Color = g.cols[i]
		imd.Push(g.pos)
		imd.Circle(float64(i+1)*g.radius/float64(len(g.cols)), 0)
	}
}

func randomNiceColor() pixel.RGBA {
again:
	r := rand.Float64()
	g := rand.Float64()
	b := rand.Float64()
	len := math.Sqrt(r*r + g*g + b*b)
	if len == 0 {
		goto again
	}

	return pixel.RGB(r/len, g/len, b/len)
}

func randomNiceBGColor() pixel.RGBA {
again:
	r := rand.Float64()
	g := rand.Float64()
	b := rand.Float64()
	len := math.Sqrt(r*r + g*g + b*b)
	if len == 0 {
		goto again
	}

	return pixel.RGB((r/len)-0.5, (g/len)-0.5, (b/len)-0.5)
}

func run() {
	rand.Seed(time.Now().UnixNano())

	sheet, anims, err := loadAnimationSheet("sheet.png", "sheet.csv", 12)
	if err != nil {
		panic(err)
	}

	cfg := pixelgl.WindowConfig{
		Title:  "Platformer",
		Bounds: pixel.R(0, 0, 1024, 768),
		VSync:  true,
	}
	win, err := pixelgl.NewWindow(cfg)
	if err != nil {
		panic(err)
	}

	phys := &gopherPhys{
		gravity:   -512,
		runSpeed:  64,
		jumpSpeed: 192,
		rect:      pixel.R(-6, -7, 6, 7),
	}

	anim := &gopherAnim{
		sheet: sheet,
		anims: anims,
		rate:  1.0 / 10,
		dir:   +1,
	}

	// hardcoded platforms
	platforms0 := []platform{
		// level0 - welcome
		{rect: pixel.R(-40, -34, 50, -32)},   // start/right platform
		{rect: pixel.R(-150, -14, -50, -12)}, // left platform
	}
	for i := range platforms0 {
		platforms0[i].color = randomNiceColor()
	}
	platforms1 := []platform{
		// level1 - start
		{rect: pixel.R(-115, -34, 20, -32)}, // start platform
	}
	for i := range platforms1 {
		platforms1[i].color = randomNiceColor()
	}
	platforms2 := []platform{
		// level3
		{rect: pixel.R(-50, -37, 50, -35)}, // start platform
		{rect: pixel.R(70, -7, 80, -5)},    // first platform
		{rect: pixel.R(40, 25, 50, 27)},    // second platform
		{rect: pixel.R(70, 55, 80, 57)},    // third platform
		{rect: pixel.R(100, 85, 200, 87)},  // end platform
	}
	for i := range platforms2 {
		platforms2[i].color = randomNiceColor()
	}

	bgcolour = randomNiceBGColor()

	// set default goal
	gol := &goal{
		pos:    pixel.V(-130, 10),
		radius: 18,
		step:   1.0 / 7,
	}

	canvas := pixelgl.NewCanvas(pixel.R(-160/2, -120/2, 160/2, 120/2))
	imd := imdraw.New(sheet)
	imd.Precision = 32

	camPos := pixel.ZV

	last := time.Now()
	for !win.Closed() {
		dt := time.Since(last).Seconds()
		last = time.Now()

		// lerp the camera position towards the gopher
		camPos = pixel.Lerp(camPos, phys.rect.Center(), 1-math.Pow(1.0/128, dt))
		cam := pixel.IM.Moved(camPos.Scaled(-1))
		canvas.SetMatrix(cam)

		// slow motion with tab
		if win.Pressed(pixelgl.KeyTab) {
			dt /= 8
		}

		// restart the platforms0 on pressing enter
		if win.JustPressed(pixelgl.KeyEnter) {
			phys.rect = phys.rect.Moved(phys.rect.Center().Scaled(-1))
			phys.vel = pixel.ZV
		}

		// control the gopher with keys
		ctrl := pixel.ZV
		if win.Pressed(pixelgl.KeyLeft) || win.Pressed(pixelgl.KeyA) {
			ctrl.X--
		}
		if win.Pressed(pixelgl.KeyRight) || win.Pressed(pixelgl.KeyD) {
			ctrl.X++
		}
		if win.JustPressed(pixelgl.KeyUp) || win.JustPressed(pixelgl.KeyW) || win.Pressed(pixelgl.KeyRightShift) {
			ctrl.Y = 1
		}

		// changes platforms based on level number
		platforms := []platform{}
		switch {
		case level == 1:
			platforms = platforms1
			gol.pos = pixel.V(-75, -75)
		case level == 2:
			platforms = platforms2
			gol.pos = pixel.V(180, 120)

		default:
			platforms = platforms0
		}

		// update the physics and animation
		phys.update(dt, ctrl, platforms, phys, gol)
		gol.update(dt)
		anim.update(dt, phys)

		// draw the scene to the canvas using IMDraw
		canvas.Clear(bgcolour)
		imd.Clear()
		for _, p := range platforms {
			p.draw(imd)
		}
		gol.draw(imd)
		anim.draw(imd, phys)
		imd.Draw(canvas)

		// stretch the canvas to the window
		//win.Clear(colornames.Blue)
		win.SetMatrix(pixel.IM.Scaled(pixel.ZV,
			math.Min(
				win.Bounds().W()/canvas.Bounds().W(),
				win.Bounds().H()/canvas.Bounds().H(),
			),
		).Moved(win.Bounds().Center()))

		canvas.Draw(win, pixel.IM.Moved(canvas.Bounds().Center()))

		win.Update()
	}
}

func main() {
	pixelgl.Run(run)
}
