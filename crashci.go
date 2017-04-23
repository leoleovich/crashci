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
const maxRoundRunningTimeSec = 10
const framesPerSecond = 2
const mapWidth = 179
const mapHeight = 38
const nameTableWidth = 30

var clear = []byte{27, 91, 50, 74, 27, 91, 72}

type Config struct {
	Log *log.Logger
}

const (
	COMPILING = 1 + iota
	WAITING
	RUNNING
	FINISHED
)

type Point struct {
	X, Y int
}

type Car struct {
	Position  Point
	Direction int
}

type Player struct {
	Conn     net.Conn
	Name     string
	Health   int64
	Bot      bool
	BotReady bool
	Car      Car
}

type Round struct {
	Players         []Player
	State           int
	LastStateChange time.Time
	Map             []byte
}

func getPlayerData(conn net.Conn) Player {
	// Get data of player and return the structure
	return Player{Conn: conn, BotReady: true, Name: fmt.Sprintf("Test %d", rand.Intn(100)), Health: 100}
}

func generateBot() Player {
	// Get data of player and return the structure
	return Player{Name: fmt.Sprintf("Bot %d", rand.Intn(100)+1), Health: 100, Bot: true}
}

func (p *Player) checkBestRoundForPlayer(round chan Round) {
	for r := range round {
		if len(r.Players) <= maxPlayersPerRound {
			r.Players = append(r.Players, *p)
			round <- r
			break
		} else {
			round <- r
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
				for _, player := range r.Players {
					player.Conn.Write([]byte(fmt.Sprintf(
						"Waiting %d seconds for other players to join\n",
						r.LastStateChange.Unix()+maxRoundWaitingTimeSec-time.Now().Unix())))
				}
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
			for ;len(round.Players) < maxPlayersPerRound; {
				round.Players = append(round.Players, generateBot())
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
func (round Round) applyNames() {
	for line, player := range round.Players {
		for i, char := range []byte(player.Name) {
			round.Map[(line+1)*mapWidth+(mapWidth-nameTableWidth+1)+i] = char
		}
		round.Map[(line+1)*mapWidth+(mapWidth-nameTableWidth+1)+len(player.Name)] = byte(':')
	}
}

func (round Round) applyHealth() {
	for line, player := range round.Players {
		health := []byte(fmt.Sprintf("%3d",player.Health))
		for i, char := range health {
			round.Map[(line+1)*mapWidth+(mapWidth-3)-len(health)+i] = char
		}
	}
}

func (round Round) start() {
	round.generateMap()
	round.applyNames()

	for {
		secondsLeft := round.LastStateChange.Unix() + maxRoundRunningTimeSec - time.Now().Unix()
		if secondsLeft <= 0 {
			round.over()
			return
		}

		round.applyHealth()

		for _, player := range round.Players {
			if player.Bot {
				continue
			}
			player.Conn.Write(clear)
			player.Conn.Write(round.Map)
			//player.Conn.Write([]byte(
			//	fmt.Sprintf("Welcome to the round. You have %d seconds to play\n", secondsLeft)))
		}

		time.Sleep(1 % framesPerSecond * 100 * time.Millisecond)
	}

}

func (round *Round) over() {
	for _, player := range round.Players {
		if player.Bot {
			continue
		}
		player.Conn.Write([]byte("Time is out\n"))
		player.Conn.Close()
	}
	round.State = FINISHED
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
