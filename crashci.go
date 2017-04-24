package main

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"time"
)

const maxParallelRounds = 100
const maxPlayersPerRound = 5
const minPlayersPerRound = 1
const maxRoundWaitingTimeSec = 5
const maxRoundRunningTimeSec = 60
const framesPerSecond = 2
const mapWidth = 179
const mapHeight = 38
const nameTableWidth = 30
const maxSpeed = 5
const horizontalCarWidth = 2
const horizontalCarHeight = 2
const verticalCarWidth = 2
const verticalCarHeight = 2

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

// Damage points
const (
	BACK = 1 + iota
	FRONT
	SIDE
)

var cars = [][]byte{
	[]byte("<|\n<|"),
	[]byte("|>\n|>"),
	[]byte("^^\n__"),
	[]byte("--\nvv")}

var clear = []byte{27, 91, 50, 74, 27, 91, 72}

type Config struct {
	Log *log.Logger
}

type Point struct {
	X, Y int
}

type Car struct {
	Position  Point
	Direction int
	Speed     int64
}

type Player struct {
	Conn      net.Conn
	Name      string
	Health    int64
	LastCrash int64
	Bot       bool
	BotReady  bool
	Car       Car
}

type Round struct {
	Players         []Player
	State           int
	LastStateChange time.Time
	Map             []byte
}

func getPlayerData(conn net.Conn) Player {
	// Get data of player and return the structure
	return Player{Conn: conn, BotReady: true, Name: fmt.Sprintf("Test %d", rand.Intn(100)), Health: 100, Car: Car{Speed: 1}}
}

func generateBot() Player {
	// Get data of player and return the structure
	return Player{Name: fmt.Sprintf("Bot %d", rand.Intn(100)+1), Health: 100, Bot: true, Car: Car{Speed: 1}}
}

func (p *Player) checkBestRoundForPlayer(round chan Round) {
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
		p.Car.Position.X, p.Car.Position.Y = 1, 1
		p.Car.Direction = RIGHT
	case 1:
		p.Car.Position.X, p.Car.Position.Y = mapWidth-nameTableWidth-verticalCarWidth, 1
		p.Car.Direction = DOWN
	case 2:
		p.Car.Position.X, p.Car.Position.Y = mapWidth-nameTableWidth-verticalCarWidth, mapHeight-verticalCarHeight-1
		p.Car.Direction = LEFT
	case 3:
		p.Car.Position.X, p.Car.Position.Y = 1, mapHeight-verticalCarHeight-1
		p.Car.Direction = UP
	case 4:
		p.Car.Position.X, p.Car.Position.Y = (mapWidth-nameTableWidth)/2, mapHeight/2
		p.Car.Direction = DOWN
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
					r.LastStateChange.Unix()+maxRoundWaitingTimeSec-time.Now().Unix())))
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

func (round *Round) generateMap() {
	for row := 0; row < mapHeight; row++ {
		for column := 0; column < mapWidth; column++ {
			var symbol byte
			if (row == 0 || row == mapHeight-1) && column < mapWidth-2 {
				symbol = byte('-')
			} else if column == 0 || column == mapWidth-3 || column == mapWidth-nameTableWidth {
				symbol = byte('|')
			} else if column == mapWidth-2 {
				symbol = byte('\r')
			} else if column == mapWidth-1 {
				symbol = byte('\n')
			} else {
				symbol = byte(' ')
			}
			round.Map[row*mapWidth+column] = symbol
		}
	}
}
func (round *Round) applyNames() {
	for line, player := range round.Players {
		for i, char := range []byte(player.Name) {
			round.Map[(line+1)*mapWidth+(mapWidth-nameTableWidth+1)+i] = char
		}
		round.Map[(line+1)*mapWidth+(mapWidth-nameTableWidth+1)+len(player.Name)] = byte(':')
	}
}

func applyHealth(round *Round, activeMap []byte) {
	for line, player := range round.Players {
		health := []byte(fmt.Sprintf("%3d", player.Health))
		for i, char := range health {
			activeMap[(line+1)*mapWidth+(mapWidth-3)-len(health)+i] = char
		}
	}
}

func applyCars(round *Round, activeMap []byte) {
	for _, player := range round.Players {
		if player.Health <= 0 {
			continue
		}
		charPosX, charPosY := 0, 0
		for _, char := range cars[player.Car.Direction] {
			if char == byte('\n') {
				charPosY++
				charPosX = 0
				continue
			}
			activeMap[(player.Car.Position.Y+charPosY)*mapWidth+player.Car.Position.X+charPosX] = char
			charPosX++
		}
	}
}

func (player *Player) checkCar(round *Round) {

	go player.checkSpeed()

	for {
		switch player.Car.Direction {
		case RIGHT:
			if player.Car.Position.X+1 >= mapWidth-nameTableWidth-horizontalCarWidth {
				player.Health = player.Health - FRONT*player.Car.Speed
				player.Car.Direction = LEFT
				player.LastCrash = time.Now().Unix()
			} else if player.hitAnotherCar(round) {
				player.Car.Direction = LEFT
				player.LastCrash = time.Now().Unix()
			} else {
				player.Car.Position.X++
			}
		case LEFT:
			if player.Car.Position.X-1 == 0 {
				player.Health = player.Health - FRONT*player.Car.Speed
				player.Car.Direction = RIGHT
				player.LastCrash = time.Now().Unix()
			} else if player.hitAnotherCar(round) {
				player.Car.Direction = RIGHT
				player.LastCrash = time.Now().Unix()
			} else {
				player.Car.Position.X--
			}
		case UP:
			if player.Car.Position.Y-1 == 0 {
				player.Health = player.Health - FRONT*player.Car.Speed
				player.Car.Direction = DOWN
				player.LastCrash = time.Now().Unix()
			} else if player.hitAnotherCar(round) {
				player.Car.Direction = DOWN
				player.LastCrash = time.Now().Unix()
			} else {
				player.Car.Position.Y--
			}
		case DOWN:
			if player.Car.Position.Y+1 >= mapHeight-verticalCarHeight {
				player.Health = player.Health - FRONT*player.Car.Speed
				player.Car.Direction = UP
				player.LastCrash = time.Now().Unix()
			} else if player.hitAnotherCar(round) {
				player.Car.Direction = UP
				player.LastCrash = time.Now().Unix()
			} else {
				player.Car.Position.Y++
			}
		}

		if player.Health <= 0 {
			return
		}
		time.Sleep(time.Duration(100/player.Car.Speed*2) * time.Millisecond)
	}
}

/*
* Check if we hit another car and how many points should be subtracted
* We subtract point from both cars in this function
 */
func (player *Player) hitAnotherCar(round *Round) bool {
	for _, p := range round.Players {
		if p == *player {
			continue
		}
		return false
	}
	return false
}

func (player *Player) checkSpeed() {
	for {
		now := time.Now().Unix()
		if now-player.LastCrash > player.Car.Speed*2 && player.Car.Speed < maxSpeed {
			player.Car.Speed++
		} else if now-player.LastCrash < 2 {
			player.Car.Speed = 1
		}
		time.Sleep(1 % framesPerSecond * 100 * time.Millisecond)
	}
}

func (round Round) start() {
	round.generateMap()
	round.applyNames()

	for i := range round.Players {
		go round.Players[i].checkCar(&round)
	}

	for {
		activeMap := make([]byte, len(round.Map))
		copy(activeMap, round.Map)

		secondsLeft := round.LastStateChange.Unix() + maxRoundRunningTimeSec - time.Now().Unix()
		if secondsLeft <= 0 {
			round.over()
			return
		}

		applyHealth(&round, activeMap)
		applyCars(&round, activeMap)

		round.writeToAllPlayers(activeMap)

		time.Sleep(1 % framesPerSecond * 100 * time.Millisecond)
	}

}

func (round *Round) over() {
	round.writeToAllPlayers([]byte("Time is out\n"))
	round.State = FINISHED
	for _, player := range round.Players {
		if player.Bot {
			continue
		}
		player.Conn.Close()
	}
}

func (round *Round) writeToAllPlayers(message []byte) {
	for _, player := range round.Players {
		if player.Bot {
			continue
		}

		go func(message []byte, player Player) {
			player.Conn.Write(clear)
			player.Conn.Write(message)
		}(message, player)
	}
}

func main() {
	logfile, err := os.OpenFile("/var/log/crashci.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	conf := &Config{log.New(logfile, "", log.Ldate|log.Lmicroseconds|log.Lshortfile)}
	l, err := net.Listen("tcp", ":4242")
	if err != nil {
		os.Exit(2)
	}
	defer l.Close()

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
			r := Round{State: COMPILING, Map: make([]byte, mapWidth*mapHeight)}
			compileRoundChannel <- r
		}

		p.checkBestRoundForPlayer(compileRoundChannel)
	}
}
