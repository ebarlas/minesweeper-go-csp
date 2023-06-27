package main

import (
	"fmt"
	"github.com/veandco/go-sdl2/img"
	"github.com/veandco/go-sdl2/sdl"
	"math/rand"
	"os"
	"sync"
	"time"
)

// cell

type CellState struct {
	row           int
	col           int
	mine          bool
	revealed      bool
	flagged       bool
	neighborMines int
}

type CellSignal int

const (
	LeftClick CellSignal = iota
	RightClick
	NeighborReveal
	NeighborFlagSet
	NeighborFlagUnset
	Stop
)

func runCell(row int, col int, mine bool, neighborMines int, inChannel <-chan CellSignal, outChannels []chan<- CellSignal, stateChannel chan<- interface{}) {
	revealed := false
	flagged := false
	neighborFlags := 0
	tellNeighbors := func(cs CellSignal) {
		for _, ch := range outChannels {
			ch <- cs
		}
	}
	revealNeighbors := func() {
		tellNeighbors(NeighborReveal)
	}
	revealMe := func() {
		if !flagged && !revealed {
			revealed = true
			if !mine && neighborMines == 0 {
				revealNeighbors()
			}
		}
	}
	for signal := range inChannel {
		switch signal {
		case LeftClick:
			if revealed && neighborMines == neighborFlags {
				revealNeighbors()
			} else {
				revealMe()
			}
		case RightClick:
			if revealed && neighborMines == neighborFlags {
				revealNeighbors()
			} else if !revealed {
				flagged = !flagged
				if flagged {
					tellNeighbors(NeighborFlagSet)
				} else {
					tellNeighbors(NeighborFlagUnset)
				}
			}
		case NeighborReveal:
			revealMe()
		case NeighborFlagSet:
			neighborFlags++
		case NeighborFlagUnset:
			neighborFlags--
		case Stop:
			return
		}
		stateChannel <- CellState{row, col, mine, revealed, flagged, neighborMines}
	}
}

// game

type ClickSide int

const (
	Left ClickSide = iota
	Right
)

type PlayState int

const (
	Init PlayState = iota
	Playing
	Won
	Lost
)

type TilePress struct {
	row       int
	col       int
	clickSide ClickSide
}

type GameState struct {
	cellStates [][]CellState
	state      PlayState
	elapsed    time.Duration
}

func (g *GameState) countFlags() int {
	sum := 0
	for _, row := range g.cellStates {
		for _, cell := range row {
			if cell.flagged {
				sum++
			}
		}
	}
	return sum
}

type ClockTick struct {
}

type FacePress struct {
}

func restartNewCells(rows int, cols int, numMines int, stateChannel chan<- interface{}) ([][]CellState, [][]chan CellSignal) {
	mines := placeMines(rows, cols, numMines)
	cellChannels := makeCellChannels(rows, cols)
	neighborMines := func(row int, col int) int {
		sum := 0
		forEachNeighbor(row, col, rows, cols, func(r int, c int) {
			if mines[r][c] {
				sum++
			}
		})
		return sum
	}
	neighborChannels := func(row int, col int) []chan<- CellSignal {
		var chs []chan<- CellSignal
		forEachNeighbor(row, col, rows, cols, func(r int, c int) {
			chs = append(chs, cellChannels[r][c])
		})
		return chs
	}
	cellStates := make([][]CellState, rows)
	for r := range cellStates {
		cellStates[r] = make([]CellState, cols)
		for c := range cellStates[r] {
			cellStates[r][c] = CellState{r, c, mines[r][c], false, false, neighborMines(r, c)}
			go runCell(r, c, cellStates[r][c].mine, cellStates[r][c].neighborMines, cellChannels[r][c], neighborChannels(r, c), stateChannel)
		}
	}
	return cellStates, cellChannels
}

func forEachNeighbor(row int, col int, rows int, cols int, fn func(int, int)) {
	for r := row - 1; r <= row+1; r++ {
		for c := col - 1; c <= col+1; c++ {
			if r >= 0 && r < rows && c >= 0 && c < cols && (r != row || c != col) {
				fn(r, c)
			}
		}
	}
}

func makeCellChannels(rows int, cols int) [][]chan CellSignal {
	channels := make([][]chan CellSignal, rows)
	for r := range channels {
		channels[r] = make([]chan CellSignal, cols)
		for c := range channels[r] {
			channels[r][c] = make(chan CellSignal, 100)
		}
	}
	return channels
}

func placeMines(rows int, cols int, numMines int) [][]bool {
	mines := make([][]bool, rows)
	for r := range mines {
		mines[r] = make([]bool, cols)
	}
	for numMines > 0 {
		r := rand.Intn(rows)
		c := rand.Intn(cols)
		if !mines[r][c] {
			mines[r][c] = true
			numMines--
		}
	}
	return mines
}

func lost(cellStates [][]CellState) bool {
	for _, row := range cellStates {
		for _, cell := range row {
			if cell.mine && cell.revealed {
				return true
			}
		}
	}
	return false
}

func countRevealed(cellStates [][]CellState) int {
	sum := 0
	for _, row := range cellStates {
		for _, cell := range row {
			if cell.revealed {
				sum++
			}
		}
	}
	return sum
}

func copyCellStates(rows int, cols int, cellStates [][]CellState) [][]CellState {
	cs := make([][]CellState, rows)
	for r := range cellStates {
		cs[r] = make([]CellState, cols)
		for c := range cellStates[r] {
			cs[r][c] = cellStates[r][c]
		}
	}
	return cs
}

func stopCells(cellChannels [][]chan CellSignal) {
	for _, row := range cellChannels {
		for _, ch := range row {
			ch <- Stop
		}
	}
}

func runGame(rows int, cols int, numMines int, gameChannel chan interface{}, windowChannel chan<- GameState) {
	playState := Init
	cellStates, cellChannels := restartNewCells(rows, cols, numMines, gameChannel)
	var startTime time.Time
	var elapsedTime time.Duration
	won := func() bool {
		return countRevealed(cellStates) == rows*cols-numMines
	}
	updateWindow := func() {
		windowChannel <- GameState{copyCellStates(rows, cols, cellStates), playState, elapsedTime}
	}
	for signal := range gameChannel {
		switch v := signal.(type) {
		case TilePress:
			if v.clickSide == Left && playState == Init {
				playState = Playing
				startTime = time.Now()
			}
			if playState == Playing {
				if v.clickSide == Left {
					cellChannels[v.row][v.col] <- LeftClick
				} else {
					cellChannels[v.row][v.col] <- RightClick
				}
			}
		case CellState:
			cellStates[v.row][v.col] = v
			if playState == Playing {
				if lost(cellStates) {
					playState = Lost
					stopCells(cellChannels)
				} else if won() {
					playState = Won
					stopCells(cellChannels)
				}
			}
			updateWindow()
		case ClockTick:
			if playState == Playing {
				elapsedTime = time.Now().Sub(startTime)
				updateWindow()
			}
		case FacePress:
			playState = Init
			elapsedTime = 0
			cellStates, cellChannels = restartNewCells(rows, cols, numMines, gameChannel)
			updateWindow()
		}
	}
}

// window

type Mode int

const (
	Beginner Mode = iota
	Intermediate
	Advanced
)

const (
	GridLeft         = 15
	GridTop          = 81
	CellSide         = 20
	FaceTop          = 18
	FaceSide         = 42
	TileSide         = 20
	DigitPanelWidth  = 65
	DigitPanelHeight = 37
	DigitPanelTop    = 21
	DigitPanelMargin = 2
	DigitWidth       = 19
	DigitHeight      = 33
)

type Dim int

const (
	Width Dim = iota
	Height
	FaceLeft
	FlagsPanelLeft
	Rows
	Cols
	Mines
)

var Dims = map[Dim]map[Mode]int32{
	Width: {
		Beginner:     210,
		Intermediate: 350,
		Advanced:     630,
	},
	Height: {
		Beginner:     276,
		Intermediate: 416,
		Advanced:     416,
	},
	FaceLeft: {
		Beginner:     84,
		Intermediate: 154,
		Advanced:     273,
	},
	FlagsPanelLeft: {
		Beginner:     16,
		Intermediate: 20,
		Advanced:     20,
	},
	Rows: {
		Beginner:     9,
		Intermediate: 16,
		Advanced:     16,
	},
	Cols: {
		Beginner:     9,
		Intermediate: 16,
		Advanced:     30,
	},
	Mines: {
		Beginner:     10,
		Intermediate: 40,
		Advanced:     99,
	},
}

func (m Mode) width() int32 {
	return Dims[Width][m]
}

func (m Mode) height() int32 {
	return Dims[Height][m]
}

func (m Mode) faceLeft() int32 {
	return Dims[FaceLeft][m]
}

func (m Mode) flagsPanelLeft() int32 {
	return Dims[FlagsPanelLeft][m]
}

func (m Mode) rows() int {
	return int(Dims[Rows][m])
}

func (m Mode) cols() int {
	return int(Dims[Cols][m])
}

func (m Mode) mines() int {
	return int(Dims[Mines][m])
}

type Assets struct {
	backgrounds [3]*sdl.Texture
	digits      [10]*sdl.Texture
	digitPanel  *sdl.Texture
	faceSad     *sdl.Texture
	faceHappy   *sdl.Texture
	faceCool    *sdl.Texture
	tileCovered *sdl.Texture
	tileFlag    *sdl.Texture
	tileMine    *sdl.Texture
	tiles       [9]*sdl.Texture
}

const Dir = "images"
const Ext = ".png"

func loadImage(renderer *sdl.Renderer, name string) *sdl.Texture {
	image, err := img.Load(Dir + "/" + name + Ext)
	if err != nil {
		panic(err)
	}
	texture, err := renderer.CreateTextureFromSurface(image)
	if err != nil {
		panic(err)
	}
	return texture
}

func (a *Assets) load(renderer *sdl.Renderer) {
	load := func(name string) *sdl.Texture {
		return loadImage(renderer, name)
	}
	for i := 0; i < 10; i++ {
		a.digits[i] = load(fmt.Sprintf("digit_%d", i))
	}
	for i := 0; i < 9; i++ {
		a.tiles[i] = load(fmt.Sprintf("tile_%d", i))
	}
	a.backgrounds[0] = load("background_small")
	a.backgrounds[1] = load("background_medium")
	a.backgrounds[2] = load("background_large")
	a.digitPanel = load("digit_panel")
	a.faceSad = load("face_lose")
	a.faceCool = load("face_win")
	a.faceHappy = load("face_playing")
	a.tileCovered = load("tile")
	a.tileFlag = load("tile_flag")
	a.tileMine = load("tile_mine")
}

func (a *Assets) unload() {
	for _, texture := range a.digits {
		texture.Destroy()
	}
	for _, texture := range a.tiles {
		texture.Destroy()
	}
	a.digitPanel.Destroy()
	a.faceSad.Destroy()
	a.faceCool.Destroy()
	a.faceHappy.Destroy()
	a.tileCovered.Destroy()
	a.tileFlag.Destroy()
	a.tileMine.Destroy()
}

func drawImage(renderer *sdl.Renderer, image *sdl.Texture, x int32, y int32, width int32, height int32) {
	src := sdl.Rect{0, 0, width, height}
	dst := sdl.Rect{x, y, width, height}
	renderer.Copy(image, &src, &dst)
}

func drawDigits(renderer *sdl.Renderer, assets *Assets, numDigits int, right int32, top int32, width int32, val int) {
	for i := 0; i < numDigits; i++ {
		digit := val % 10
		drawImage(renderer, assets.digits[digit], right-width, top, DigitWidth, DigitHeight)
		val /= 10
		right -= width
	}
}

func drawFlagsPanel(renderer *sdl.Renderer, assets *Assets, mode Mode, gameState *GameState) {
	drawImage(renderer, assets.digitPanel, mode.flagsPanelLeft(), DigitPanelTop, DigitPanelWidth, DigitPanelHeight)
	flags := 0
	if gameState != nil {
		flags = gameState.countFlags()
	}
	flagsRemaining := mode.mines() - flags
	if flagsRemaining < 0 {
		flagsRemaining = 0
	}
	drawDigits(
		renderer,
		assets,
		3,
		mode.flagsPanelLeft()+DigitPanelWidth,
		DigitPanelMargin+DigitPanelTop,
		DigitPanelMargin+DigitWidth,
		flagsRemaining)
}

func drawTimePanel(renderer *sdl.Renderer, assets *Assets, mode Mode, gameState *GameState) {
	drawImage(renderer, assets.digitPanel, mode.width()-mode.flagsPanelLeft()-DigitPanelWidth, DigitPanelTop, DigitPanelWidth, DigitPanelHeight)
	elapsed := 0
	if gameState != nil {
		elapsed = int(gameState.elapsed.Seconds())
	}
	if elapsed > 999 {
		elapsed = 999
	}
	drawDigits(
		renderer,
		assets,
		3,
		mode.width()-mode.flagsPanelLeft(),
		DigitPanelMargin+DigitPanelTop,
		DigitPanelMargin+DigitWidth,
		elapsed)
}

func drawFace(renderer *sdl.Renderer, assets *Assets, mode Mode, gameState *GameState) {
	faceImage := assets.faceHappy
	if gameState != nil {
		if gameState.state == Won {
			faceImage = assets.faceCool
		} else if gameState.state == Lost {
			faceImage = assets.faceSad
		}
	}
	drawImage(renderer, faceImage, mode.faceLeft(), FaceTop, FaceSide, FaceSide)
}

func tileImage(row int, col int, gameState *GameState, assets *Assets) *sdl.Texture {
	if gameState == nil {
		return assets.tileCovered
	}
	cs := &gameState.cellStates[row][col]
	if cs.revealed {
		if cs.mine {
			return assets.tileMine
		} else {
			return assets.tiles[cs.neighborMines]
		}
	} else {
		if cs.flagged {
			return assets.tileFlag
		} else {
			return assets.tileCovered
		}
	}
}

func drawTiles(renderer *sdl.Renderer, assets *Assets, mode Mode, gameState *GameState) {
	for r := 0; r < mode.rows(); r++ {
		for c := 0; c < mode.cols(); c++ {
			drawImage(renderer, tileImage(r, c, gameState, assets), int32(GridLeft+c*CellSide), int32(GridTop+r*CellSide), TileSide, TileSide)
		}
	}
}

func draw(mode Mode, renderer *sdl.Renderer, assets *Assets, gameState *GameState) {
	drawImage(renderer, assets.backgrounds[mode], 0, 0, mode.width(), mode.height())
	drawFlagsPanel(renderer, assets, mode, gameState)
	drawTimePanel(renderer, assets, mode, gameState)
	drawFace(renderer, assets, mode, gameState)
	drawTiles(renderer, assets, mode, gameState)
}

type Window struct {
	window   *sdl.Window
	renderer *sdl.Renderer
	assets   Assets
	mode     Mode
}

func (w *Window) load(mode Mode) {
	var err error
	if err = sdl.Init(sdl.INIT_VIDEO); err != nil {
		panic(err)
	}
	w.window, err = sdl.CreateWindow("Minesweeper", sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED, mode.width(), mode.height(), sdl.WINDOW_SHOWN)
	if err != nil {
		panic(err)
	}
	w.renderer, err = sdl.CreateRenderer(w.window, -1, sdl.RENDERER_ACCELERATED)
	if err != nil {
		panic(err)
	}
	w.assets.load(w.renderer)
}

func (w *Window) unload() {
	w.assets.unload()
	w.renderer.Destroy()
	w.window.Destroy()
	sdl.Quit()
}

type Sync struct {
	state   *GameState
	mu      sync.Mutex
	version int
}

func (s *Sync) set(state *GameState) {
	s.mu.Lock()
	s.state = state
	s.version++
	s.mu.Unlock()
}

func (s *Sync) get() (*GameState, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.version
}

func insideRect(left int32, top int32, width int32, height int32, x int32, y int32) bool {
	return x >= left && y >= top && x < left+width && y < top+height
}

func onMouseEvent(e *sdl.MouseButtonEvent, mode Mode, gameChannel chan<- interface{}) {
	if e.Type == sdl.MOUSEBUTTONUP {
		if insideRect(mode.faceLeft(), FaceTop, FaceSide, FaceSide, e.X, e.Y) {
			gameChannel <- FacePress{}
		} else {
			row := int((e.Y - GridTop) / TileSide)
			col := int((e.X - GridLeft) / TileSide)
			clickSide := Left
			if e.Button == sdl.BUTTON_RIGHT {
				clickSide = Right
			}
			if row >= 0 && row < mode.rows() && col >= 0 && col < mode.cols() {
				gameChannel <- TilePress{row, col, clickSide}
			}
		}
	}

}

func runWindow(mode Mode, sync *Sync, gameChannel chan<- interface{}) {
	lastVersion := -1
	var window Window
	window.load(mode)
	defer window.unload()
	for {
		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			switch e := event.(type) {
			case *sdl.QuitEvent:
				return
			case *sdl.MouseButtonEvent:
				onMouseEvent(e, mode, gameChannel)
			}
		}
		state, version := sync.get()
		if version > lastVersion {
			window.renderer.Clear()
			draw(mode, window.renderer, &window.assets, state)
			window.renderer.Present()
		}
		lastVersion = version
	}
}

// main

func runClock(gameChannel chan<- interface{}) {
	for {
		gameChannel <- ClockTick{}
		time.Sleep(200 * time.Millisecond)
	}
}

func runWindowSync(windowChannel <-chan GameState, sync *Sync) {
	for state := range windowChannel {
		sync.set(&state)
	}
}

func modeFromArgs() Mode {
	var arg string
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}
	switch arg {
	case "Beginner":
		return Beginner
	case "Advanced":
		return Advanced
	default:
		return Intermediate
	}
}

func main() {
	mode := modeFromArgs()
	var sync Sync
	gameChannel := make(chan interface{})
	windowChannel := make(chan GameState)
	go runGame(mode.rows(), mode.cols(), mode.mines(), gameChannel, windowChannel)
	go runClock(gameChannel)
	go runWindowSync(windowChannel, &sync)
	runWindow(mode, &sync, gameChannel)
}
