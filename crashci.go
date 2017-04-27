package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"time"
)

const framesPerSecond = 5
const maxParallelRounds = 100
const maxPlayersPerRound = 5
const minPlayersPerRound = 1
const maxRoundWaitingTimeSec = 5
const maxRoundRunningTimeSec = 600
const mapWidth = 179
const mapHeight = 38
const nameTableWidth = 30
const maxSpeed = 5
const horizontalCarWidth = 7
const horizontalCarHeight = 3
const verticalCarWidth = 5
const verticalCarHeight = 3

const colorPrefix = "\x1b["
const colorPostfix = "m"

// States of the round
const (
	COMPILING = 1 + iota
	WAITING
	RUNNING
	FINISHED
)

// Directions
const (
	LEFT = 0 + iota
	RIGHT
	UP
	DOWN
)

// Car Borders
const (
	LEFTUP = 0 + iota
	RIGHTUP
	RIGHTDOWN
	LEFTDOWN
)

// Damage points
const (
	BACK = 1 + iota
	FRONT
	SIDE
)

// Colors
const (
	RESET = 0
	BOLD  = 1
	RED   = 31 + iota
	GREEN
	YELLOW
	BLUE
	MAGENTA
)

var cars = [4][]byte{}

//var clear = []byte{27, 91, 50, 74, 27, 91, 72}
var home = []byte{27, 91, 72}
var clear = []byte{27, 91, 50, 74}

type Config struct {
	Log      *log.Logger
	AcidPath string
}

type Point struct {
	X, Y int
}

type Rectangle struct {
	Points [4]Point // LeftUP, RightUP, RightDOWN, LeftDOWN
}

type Car struct {
	Borders   Rectangle
	Direction int
	Speed     int64
}

type Player struct {
	Conn      net.Conn
	Name      string
	Health    int64
	LastCrash int64
	Color     int
	Bot       bool
	BotReady  bool
	Car       Car
}

type Round struct {
	Players         []Player
	State           int
	LastStateChange time.Time
	Bonus           Point
	FrameBuffer     Symbols
}

type Symbol struct {
	Color int
	Char  []byte
}

type Symbols []Symbol

func getAcid(conf *Config, fileName string) ([]byte, error) {
	fileStat, err := os.Stat(conf.AcidPath + "/" + fileName)
	if err != nil {
		conf.Log.Printf("Acid %s does not exist: %v\n", fileName, err)
		return []byte{}, err
	}

	acid := make([]byte, fileStat.Size())
	f, err := os.OpenFile(conf.AcidPath+"/"+fileName, os.O_RDONLY, os.ModePerm)
	if err != nil {
		conf.Log.Printf("Error while opening %s: %v\n", fileName, err)
		os.Exit(1)
	}
	defer f.Close()

	f.Read(acid)

	return acid, nil
}

func getPlayerData(conn net.Conn) Player {
	// Get data of player and return the structure
	return Player{Conn: conn, BotReady: true, Name: fmt.Sprintf("Test %d", rand.Intn(100)), Health: 100, Car: Car{Speed: 1}}
}

func generateBot() Player {
	// Get data of player and return the structure
	return Player{Name: fmt.Sprintf("Bot %d", rand.Intn(100)+1), Health: 100, Bot: true, Car: Car{Speed: 1}}
}

func initTelnet(conn net.Conn) error {
	// https://tools.ietf.org/html/rfc854
	telnetOptions := []byte{
		255, 253, 34, // IAC DO LINEMODE
		255, 250, 34, 1, 0, 255, 240, // IAC SB LINEMODE MODE 0 IAC SE
		255, 251, 1, // IAC WILL ECHO
	}
	_, err := conn.Write(telnetOptions)
	if err != nil {
		return err
	}
	return nil
}

func readTelnet(conn net.Conn) error {
	// https://tools.ietf.org/html/rfc854
	reply := make([]byte, 1)
	bytesRead := 0
	shortCommand := false

	for {
		_, err := conn.Read(reply)
		if err != nil {
			return err
		}
		bytesRead++

		if reply[0] != 250 && bytesRead == 1 {
			shortCommand = true
		}

		if shortCommand && bytesRead == 2 {
			return nil
		} else if reply[0] == 240 {
			return nil
		}
	}
}

func checkRoundReady(compileRoundChannel, runningRoundChannel chan Round) {
	fmt.Println("compile/waiting rounds:", len(compileRoundChannel))
	for r := range compileRoundChannel {
		botReady := 0
		fmt.Println("players in round:", len(r.Players))
		for _, player := range r.Players {
			if player.BotReady {
				botReady++
			}
		}

		if len(r.Players) == maxPlayersPerRound ||
			(r.State == WAITING && r.LastStateChange.Add(maxRoundWaitingTimeSec*time.Second).Before(time.Now())) {
			// We are starting round if it is fully booked or waiting time is expired
			fmt.Println("Round has changed to the state RUNNING")
			r.State = RUNNING
			r.LastStateChange = time.Now()
			runningRoundChannel <- r
		} else if botReady == len(r.Players) && len(r.Players) >= minPlayersPerRound {
			if r.State == COMPILING {
				fmt.Println("Round has changed to the state WAITING")
				r.State = WAITING
				r.LastStateChange = time.Now()
			} else if r.State == WAITING {
				r.writeToAllPlayers([]byte(fmt.Sprintf(
					"Waiting %d seconds for other players to join\n",
					r.LastStateChange.Unix()+maxRoundWaitingTimeSec-time.Now().Unix())), true)
			}
			compileRoundChannel <- r
		} else {
			// Return round back
			compileRoundChannel <- r
		}
		time.Sleep(1 * time.Second)
	}
}

func checkRoundRun(runningRoundChannel chan Round) {
	for {
		for round := range runningRoundChannel {
			for len(round.Players) < maxPlayersPerRound {
				p := generateBot()
				p.initPlayer(len(round.Players))
				round.Players = append(round.Players, p)
			}
			go round.start()
		}
	}
}

func (round *Round) applyBonuses(activeMap []Symbol) {
	if round.State == FINISHED {
		return
	} else if round.Bonus.X == -1 && round.Bonus.Y == -1 {
		if rand.Int()%100 == 0 {
			round.Bonus = Point{rand.Intn(mapWidth-nameTableWidth-1) + 1, rand.Intn(mapHeight-1) + 1}
		}
		//activeMap[(line+1)*mapWidth+(mapWidth-3)-len(health)+i] = Symbol{player.Color, []byte{char}}
	}

}

func (round *Round) applyHealth(activeMap []Symbol) {
	for line, player := range round.Players {
		health := []byte(fmt.Sprintf("%3d", player.Health))
		for i, char := range health {
			activeMap[(line+1)*mapWidth+(mapWidth-3)-len(health)+i] = Symbol{player.Color, []byte{char}}
		}
	}
}

func (round *Round) applyCars(activeMap []Symbol) {
	for _, player := range round.Players {
		charPosX, charPosY := 0, 0
		for i := 0; i < len(cars[player.Car.Direction]); i++ {
			var chars []byte
			if cars[player.Car.Direction][i] == byte('\n') {
				charPosY++
				charPosX = 0
				continue
			} else if cars[player.Car.Direction][i] == 226 {
				/*
				 This means extended ASCII is used. After 226 2 bytes must follow
				*/
				chars = []byte{cars[player.Car.Direction][i], cars[player.Car.Direction][i+1], cars[player.Car.Direction][i+2]}
				i += 2
			} else if cars[player.Car.Direction][i] == 194 {
				/*
				 This means extended ASCII is used. After 194 1 bytes must follow
				*/
				chars = []byte{cars[player.Car.Direction][i], cars[player.Car.Direction][i+1]}
				i++
			} else if player.Health <= 0 && cars[player.Car.Direction][i] == 'o' {
				chars = []byte{'x'}
			} else {
				chars = []byte{cars[player.Car.Direction][i]}
			}
			activeMap[(player.Car.Borders.Points[LEFTUP].Y+charPosY)*mapWidth+player.Car.Borders.Points[LEFTUP].X+charPosX] = Symbol{player.Color, chars}
			charPosX++
		}
	}
}

func (rectangle *Rectangle) intersects(r *Rectangle) bool {
	if rectangle.Points[RIGHTDOWN].X < r.Points[LEFTUP].X ||
		r.Points[RIGHTDOWN].X < rectangle.Points[LEFTUP].X ||
		rectangle.Points[RIGHTDOWN].Y < r.Points[LEFTUP].Y ||
		r.Points[RIGHTDOWN].Y < rectangle.Points[LEFTUP].Y {
		return false
	}
	return true
}

func (rectangle *Rectangle) nextTo(r *Rectangle) int {
	// Return the closes side of rectangles
	if rectangle.Points[LEFTUP].X-3 <= r.Points[RIGHTDOWN].X && rectangle.Points[LEFTUP].X-3 >= r.Points[LEFTDOWN].X &&
		((rectangle.Points[LEFTUP].Y <= r.Points[RIGHTDOWN].Y && rectangle.Points[LEFTUP].Y >= r.Points[RIGHTUP].Y) ||
			(rectangle.Points[LEFTDOWN].Y <= r.Points[RIGHTDOWN].Y && rectangle.Points[LEFTDOWN].Y >= r.Points[RIGHTUP].Y)) {
		return LEFT
	} else if rectangle.Points[RIGHTUP].X+3 >= r.Points[LEFTDOWN].X && rectangle.Points[RIGHTUP].X+3 <= r.Points[RIGHTDOWN].X &&
		((rectangle.Points[LEFTUP].Y <= r.Points[RIGHTDOWN].Y && rectangle.Points[LEFTUP].Y >= r.Points[RIGHTUP].Y) ||
			(rectangle.Points[LEFTDOWN].Y <= r.Points[RIGHTDOWN].Y && rectangle.Points[LEFTDOWN].Y >= r.Points[RIGHTUP].Y)) {
		return RIGHT
	} else if rectangle.Points[LEFTUP].Y-3 <= r.Points[RIGHTDOWN].Y && rectangle.Points[LEFTUP].Y-3 >= r.Points[RIGHTUP].Y &&
		((rectangle.Points[LEFTUP].X <= r.Points[RIGHTDOWN].X && rectangle.Points[LEFTUP].X >= r.Points[LEFTDOWN].X) ||
			(rectangle.Points[RIGHTUP].X >= r.Points[LEFTDOWN].X && rectangle.Points[RIGHTUP].X <= r.Points[RIGHTDOWN].X)) {
		return UP
	} else if rectangle.Points[RIGHTDOWN].Y+3 >= r.Points[LEFTUP].Y && rectangle.Points[RIGHTDOWN].Y+3 <= r.Points[LEFTDOWN].Y &&
		((rectangle.Points[LEFTUP].X <= r.Points[RIGHTDOWN].X && rectangle.Points[LEFTUP].X >= r.Points[LEFTDOWN].X) ||
			(rectangle.Points[RIGHTUP].X >= r.Points[LEFTDOWN].X && rectangle.Points[RIGHTUP].X <= r.Points[RIGHTDOWN].X)) {
		return DOWN
	}
	return -1
}

func (symbols Symbols) symbolsToByte() []byte {
	var returnSlice []byte
	for _, symbol := range symbols {
		// Should be something like \x1b[31m^\x1b[0m for symbols with colors or ^ without
		if symbol.Color != RESET {
			returnSlice = append(returnSlice, []byte(colorPrefix+fmt.Sprintf("%d", symbol.Color)+colorPostfix)...)
		}
		returnSlice = append(returnSlice, symbol.Char...)
		if symbol.Color != RESET {
			returnSlice = append(returnSlice, []byte(colorPrefix+fmt.Sprintf("%d", RESET)+colorPostfix)...)
		}
	}
	return returnSlice
}

func (p *Player) checkBestRoundForPlayer(round chan Round) {
	// TODO: Do not assign players with the same name in the same round!!
	for r := range round {
		if len(r.Players) < maxPlayersPerRound {
			p.initPlayer(len(r.Players))
			r.Players = append(r.Players, *p)
			round <- r
			break
		} else {
			round <- r
		}
	}
}

func (p *Player) initPlayer(id int) {
	switch id {
	case 0:
		initX, initY := 1, 1
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + horizontalCarWidth, initY},
			{initX + horizontalCarWidth, initY + horizontalCarHeight},
			{initX, initY + horizontalCarHeight}}}
		p.Car.Direction = RIGHT
	case 1:
		initX, initY := mapWidth-nameTableWidth-verticalCarWidth, 1
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + verticalCarWidth, initY},
			{initX + verticalCarWidth, initY + verticalCarHeight},
			{initX, initY + verticalCarHeight}}}
		p.Car.Direction = DOWN
	case 2:
		initX, initY := mapWidth-nameTableWidth-horizontalCarWidth, mapHeight-horizontalCarHeight-1
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + horizontalCarWidth, initY},
			{initX + horizontalCarWidth, initY + horizontalCarHeight},
			{initX, initY + horizontalCarHeight}}}
		p.Car.Direction = LEFT
	case 3:
		initX, initY := 1, mapHeight-verticalCarHeight-1
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + verticalCarWidth, initY},
			{initX + verticalCarWidth, initY + verticalCarHeight},
			{initX, initY + verticalCarHeight}}}
		p.Car.Direction = UP
	case 4:
		initX, initY := (mapWidth-nameTableWidth)/2, mapHeight/2
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + verticalCarWidth, initY},
			{initX + verticalCarWidth, initY + verticalCarHeight},
			{initX, initY + verticalCarHeight}}}
		p.Car.Direction = DOWN
	}
	// Colors are sequential, so we can use first color RED and set the rest based on IDs
	p.Color = RED + id
}

func (round *Round) generateMap() {
	for row := 0; row < mapHeight; row++ {
		for column := 0; column < mapWidth; column++ {
			var char byte
			if (row == 0 || row == mapHeight-1) && column < mapWidth-2 {
				char = byte('-')
			} else if column == 0 || column == mapWidth-3 || column == mapWidth-nameTableWidth {
				char = byte('|')
			} else if column == mapWidth-2 {
				char = byte('\r')
			} else if column == mapWidth-1 {
				char = byte('\n')
			} else {
				char = byte(' ')
			}
			round.FrameBuffer[row*mapWidth+column] = Symbol{0, []byte{char}}
		}
	}
}
func (round *Round) applyNames() {
	for line, player := range round.Players {
		for i, char := range []byte(player.Name) {
			round.FrameBuffer[(line+1)*mapWidth+(mapWidth-nameTableWidth+1)+i] = Symbol{player.Color, []byte{char}}
		}
		round.FrameBuffer[(line+1)*mapWidth+(mapWidth-nameTableWidth+1)+len(player.Name)] = Symbol{RESET, []byte{':'}}
	}
}

func (car *Car) recalculateBorders(crash bool) {
	switch car.Direction {
	case RIGHT:
		for i := range car.Borders.Points {
			if crash {
				car.Borders.Points[i].X -= horizontalCarWidth
			} else {
				car.Borders.Points[i].X++
			}
		}
	case LEFT:
		for i, _ := range car.Borders.Points {
			if crash {
				car.Borders.Points[i].X += horizontalCarWidth
			} else {
				car.Borders.Points[i].X--
			}
		}
	case UP:
		for i, _ := range car.Borders.Points {
			if crash {
				car.Borders.Points[i].Y += verticalCarHeight
			} else {
				car.Borders.Points[i].Y--
			}
		}
	case DOWN:
		for i, _ := range car.Borders.Points {
			if crash {
				car.Borders.Points[i].Y -= verticalCarHeight
			} else {
				car.Borders.Points[i].Y++
			}
		}
	}
}

func (player *Player) readDirection() {
	if initTelnet(player.Conn) != nil {
		return
	}

	for {
		if player.Health <= 0 {
			return
		}
		direction := make([]byte, 1)

		// Read all possible bytes and try to find a sequence of:
		// ESC [ cursor_key
		escpos := 0
		for {
			_, err := player.Conn.Read(direction)
			if err != nil {
				player.Health = 0
				return
			}

			// Check if telnet want to negotiate something
			if escpos == 0 && direction[0] == 255 {
				readTelnet(player.Conn)
			} else if escpos == 0 && direction[0] == 27 {
				escpos = 1
			} else if direction[0] == 3 {
				// Ctrl+C
				player.Health = 0
				return
			} else if escpos == 1 && direction[0] == 91 {
				escpos = 2
			} else if escpos == 2 {
				break
			}
		}

		switch direction[0] {
		case 68:
			// Left
			if player.Car.Direction != RIGHT {
				player.Car.Direction = LEFT
			} else if player.Car.Speed > 1 {
				player.Car.Speed = 1
			}
		case 67:
			// Right
			if player.Car.Direction != LEFT {
				player.Car.Direction = RIGHT
			} else if player.Car.Speed > 1 {
				player.Car.Speed = 1
			}
		case 65:
			// Up
			if player.Car.Direction != DOWN {
				player.Car.Direction = UP
			} else if player.Car.Speed > 1 {
				player.Car.Speed = 1
			}
		case 66:
			// Down
			if player.Car.Direction != UP {
				player.Car.Direction = DOWN
			} else if player.Car.Speed > 1 {
				player.Car.Speed = 1
			}
		}
	}
}

/*
Checks of the SIDE is safe
*/
func (player *Player) checkOkSide(players []*Player, side int) bool {
	for _, p := range players {
		if p.Name == player.Name {
			continue
		}
		// If player nextTo someone from the SIDE - return false
		if side == player.Car.Borders.nextTo(&p.Car.Borders) {
			return false
		}
	}
	return true
}

func (round *Round) getPlayersExcept(exceptions []Player) []*Player {
	var rest []*Player
	for i := range round.Players {
		exception := false
		for k := range exceptions {
			if round.Players[i].Name == exceptions[k].Name {
				exception = true
			}
		}
		if !exception {
			rest = append(rest, &round.Players[i])
		}
	}
	return rest
}

func (round *Round) getRandomNonBotPlayerId() int {
	for {
		randId := rand.Intn(len(round.Players))
		targetPlayer := round.Players[randId]
		if targetPlayer.Health > 0 && !targetPlayer.Bot {
			return randId
		}
	}
}

func (player *Player) moveBot(round *Round) {
	for {
		targetPlayer := &round.Players[round.getRandomNonBotPlayerId()]
		restPlayers := round.getPlayersExcept([]Player{*targetPlayer, *player})

		for i := 0; i < 20; i++ {
			// Do not hunt dead player
			if targetPlayer.Health <= 0 {
				break
			}
			if player.Health <= 0 {
				return
			}

			// Navigate Bot based on centers of cars
			myCenter := Point{
				player.Car.Borders.Points[LEFTUP].X + (player.Car.Borders.Points[RIGHTUP].X-player.Car.Borders.Points[LEFTUP].X)/2,
				player.Car.Borders.Points[LEFTUP].Y + (player.Car.Borders.Points[LEFTDOWN].Y-player.Car.Borders.Points[LEFTUP].Y)/2,
			}
			targetCenter := Point{
				targetPlayer.Car.Borders.Points[LEFTUP].X + (targetPlayer.Car.Borders.Points[RIGHTUP].X-targetPlayer.Car.Borders.Points[LEFTUP].X)/2,
				targetPlayer.Car.Borders.Points[LEFTUP].Y + (targetPlayer.Car.Borders.Points[LEFTDOWN].Y-targetPlayer.Car.Borders.Points[LEFTUP].Y)/2,
			}

			// Chose closet way to the target
			if targetCenter.X < myCenter.X {
				// Target is left from us
				if player.Car.Direction != RIGHT && player.checkOkSide(restPlayers, LEFT) {
					player.Car.Direction = LEFT
				} else if targetCenter.Y < myCenter.Y {
					player.Car.Direction = UP
				} else {
					player.Car.Direction = DOWN
				}
			} else if targetCenter.X > myCenter.X {
				// Target is right from us
				if player.Car.Direction != LEFT && player.checkOkSide(restPlayers, RIGHT) {
					player.Car.Direction = RIGHT
				} else if targetCenter.Y < myCenter.Y {
					player.Car.Direction = UP
				} else {
					player.Car.Direction = DOWN
				}
			} else if targetCenter.Y < myCenter.Y && player.checkOkSide(restPlayers, UP) {
				// Target is above us
				if player.Car.Direction != DOWN {
					player.Car.Direction = UP
				} else if targetCenter.X > myCenter.X {
					player.Car.Direction = RIGHT
				} else {
					player.Car.Direction = LEFT
				}
			} else if targetCenter.Y > myCenter.Y && player.checkOkSide(restPlayers, DOWN) {
				// Target it below us
				if player.Car.Direction != UP {
					player.Car.Direction = DOWN
				} else if targetCenter.X > myCenter.X {
					player.Car.Direction = RIGHT
				} else {
					player.Car.Direction = LEFT
				}
			}

			time.Sleep(time.Duration(500 * time.Millisecond))
		}
	}
}

func (player *Player) checkPosition(round *Round) {
	for {
		if player.Health <= 0 {
			return
		}

		player.Car.recalculateBorders(false)
		hit := false
		for _, point := range player.Car.Borders.Points {
			if point.X < 1 || point.X > mapWidth-nameTableWidth || point.Y < 1 || point.Y > mapHeight-1 {
				// Hit the wall
				player.Health -= FRONT * player.Car.Speed
				hit = true
				break
			} else {
				// Hit another car
				for _, victim := range round.Players {
					if player.Name == victim.Name {
						continue
					}
					if player.Car.Borders.intersects(&victim.Car.Borders) {
						// Hit in the back
						if (player.Car.Direction == RIGHT && victim.Car.Direction == LEFT) ||
							(player.Car.Direction == LEFT && victim.Car.Direction == RIGHT) ||
							(player.Car.Direction == UP && victim.Car.Direction == DOWN) ||
							(player.Car.Direction == DOWN && victim.Car.Direction == UP) {
							// Face to face
							player.Health -= FRONT * (player.Car.Speed + victim.Car.Speed)
						} else if (player.Car.Direction == RIGHT && victim.Car.Direction == UP) ||
							(player.Car.Direction == LEFT && victim.Car.Direction == UP) ||
							(player.Car.Direction == RIGHT && victim.Car.Direction == DOWN) ||
							(player.Car.Direction == LEFT && victim.Car.Direction == DOWN) ||
							(player.Car.Direction == UP && victim.Car.Direction == RIGHT) ||
							(player.Car.Direction == UP && victim.Car.Direction == LEFT) ||
							(player.Car.Direction == DOWN && victim.Car.Direction == RIGHT) ||
							(player.Car.Direction == DOWN && victim.Car.Direction == LEFT) {
							// Side hit
							player.Health -= SIDE * player.Car.Speed
						} else {
							player.Health -= BACK * (player.Car.Speed - victim.Car.Speed)
						}
						hit = true
						break
					}
				}
				if hit {
					break
				}
			}
		}

		if hit {
			player.Car.recalculateBorders(true)
			switch player.Car.Direction {
			case RIGHT:
				player.Car.Direction = LEFT
			case LEFT:
				player.Car.Direction = RIGHT
			case UP:
				player.Car.Direction = DOWN
			case DOWN:
				player.Car.Direction = UP
			}
			player.LastCrash = time.Now().Unix()
		}

		/*
		 Because vertical symbols are 3x bigger, than horizontal, we need to slowdown recalculation of vertical objects
		*/
		slowerDown := int64(1)
		if player.Car.Direction == UP || player.Car.Direction == DOWN {
			slowerDown = 3
		}

		time.Sleep(time.Duration(slowerDown*100/player.Car.Speed*2) * time.Millisecond)
	}
}

func (player *Player) checkSpeed() {
	for {
		if player.Health <= 0 {
			return
		}
		now := time.Now().Unix()
		if now-player.LastCrash > player.Car.Speed*2 && player.Car.Speed < maxSpeed {
			player.Car.Speed++
		} else if now-player.LastCrash < 2 {
			player.Car.Speed = 1
		}
		time.Sleep(1 % framesPerSecond * 100 * time.Millisecond)
	}
}

func (player *Player) checkHealth() {
	for {
		if player.Health <= 0 {
			player.Health = 0
			player.Color = BOLD
			return
		}
		time.Sleep(1 % framesPerSecond * 100 * time.Millisecond)
	}
}

func (round *Round) collisionChecker() {
	var wg sync.WaitGroup
	wg.Add(1)
	for i := range round.Players {
		if round.Players[i].Bot {
			go round.Players[i].moveBot(round)
		} else {
			go round.Players[i].readDirection()
		}
		go round.Players[i].checkPosition(round)
		go round.Players[i].checkSpeed()
		go round.Players[i].checkHealth()
	}
	wg.Wait()
}

func (round *Round) checkGameOver() {
	humans := 0
	deads := 0

	for _, p := range round.Players {
		if !p.Bot {
			humans++
			if p.Health <= 0 {
				deads++
			}
		}

	}

	secondsLeft := round.LastStateChange.Unix() + maxRoundRunningTimeSec - time.Now().Unix()
	if humans == deads || secondsLeft <= 0 {
		round.State = FINISHED
		fmt.Println("Round has changed to the state FINISHED")
	}
}

func (round Round) start() {
	round.generateMap()
	round.applyNames()

	go round.collisionChecker()

	for {
		activeFrameBuffer := make(Symbols, len(round.FrameBuffer))
		copy(activeFrameBuffer, round.FrameBuffer)

		round.applyHealth(activeFrameBuffer)
		round.applyCars(activeFrameBuffer)

		round.writeToAllPlayers(activeFrameBuffer.symbolsToByte(), false)

		round.checkGameOver()
		if round.State == FINISHED {
			round.over()
			return
		}
		time.Sleep(1 % framesPerSecond * 100 * time.Millisecond)
	}

}

func (round *Round) over() {
	round.writeToAllPlayers([]byte("Time is out\n"), false)
	for _, player := range round.Players {
		if player.Bot {
			continue
		}
		player.Conn.Close()
	}
}

func (round *Round) writeToAllPlayers(message []byte, clean bool) {
	for _, player := range round.Players {
		if player.Bot {
			continue
		}

		go func(message []byte, player Player, clean bool) {
			if clean {
				player.Conn.Write(clear)
			}
			player.Conn.Write(home)
			player.Conn.Write(message)
		}(message, player, clean)
	}
}

func main() {
	// Make random unique
	rand.Seed(time.Now().Unix())

	logfile, err := os.OpenFile("/var/log/crashci.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	conf := &Config{log.New(logfile, "", log.Ldate|log.Lmicroseconds|log.Lshortfile), "/Users/leoleovich/go/src/github.com/leoleovich/crashci/artifacts"}
	l, err := net.Listen("tcp", ":4242")
	if err != nil {
		os.Exit(2)
	}
	defer l.Close()

	// Read sketches
	cars[LEFT], _ = getAcid(conf, "carLeft.txt")
	cars[RIGHT], _ = getAcid(conf, "carRight.txt")
	cars[UP], _ = getAcid(conf, "carUp.txt")
	cars[DOWN], _ = getAcid(conf, "carDown.txt")

	compileRoundChannel := make(chan Round, maxParallelRounds)
	runningRoundChannel := make(chan Round, maxParallelRounds)

	go checkRoundReady(compileRoundChannel, runningRoundChannel)
	go checkRoundRun(runningRoundChannel)

	for {
		conn, err := l.Accept()
		if err != nil {
			conf.Log.Println("Failed to accept request", err)
		}

		// Check for name and stuff. Ask if he agrees to play with bots
		p := getPlayerData(conn)

		if len(compileRoundChannel) == 0 {
			r := Round{State: COMPILING, FrameBuffer: make([]Symbol, mapWidth*mapHeight)}
			compileRoundChannel <- r
		}

		p.checkBestRoundForPlayer(compileRoundChannel)
	}
}
