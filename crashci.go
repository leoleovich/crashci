package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strings"
	"time"
)

const framesPerSecond = 8
const getReadyPause = 400
const maxNameLength = 25
const maxParallelRounds = 100
const maxPlayersPerRound = 5
const minPlayersPerRound = 1
const maxRoundWaitingTimeSec = 5
const maxRoundRunningTimeSec = 600
const maxSpeed = 5

const bonusPoint = 5
const lowFactor = 50
const highFactor = 5

const mapWidth = 179
const mapHeight = 38
const nameTableWidth = 30
const horizontalCarWidth = 7
const horizontalCarHeight = 3
const verticalCarWidth = 5
const verticalCarHeight = 3

const colorPrefix = "\x1b["
const colorPostfix = "m"
const bonus = "\xE2\x99\xA5"
const bomb = "\xE2\x9C\xB3"

var (
	cars = [4][]byte{}
	// http://www.isthe.com/chongo/tech/comp/ansi_escapes.html
	home  = []byte{27, 91, 72}
	clear = []byte{27, 91, 50, 74}
	// [14A[100D
	middle = []byte{27, 91, 49, 52, 65, 27, 91, 57, 52, 68}
	conf   Config
)

// States of the round
const (
	COMPILING = 1 + iota
	WAITING
	STARTING
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
	DAMAGE_BACK  = 2
	DAMAGE_FRONT = 4
	DAMAGE_SIDE  = 6
)

// Colors
const (
	RESET = 0
	BOLD  = 1
	RED   = 31
	GREEN = 32
	//YELLOW  = 33
	//BLUE    = 34
	//MAGENTA = 35
)

type Config struct {
	Log      *log.Logger
	AcidPath string
}

type Point struct {
	X, Y int
}

type Symbol struct {
	Color int
	Char  []byte
}

type Symbols []Symbol

func getAcid(fileName string) ([]byte, error) {
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

func getPlayerData(conn net.Conn, splash []byte) (Player, error) {
	// Get data of player and return the structure
	conn.Write(clear)
	conn.Write(home)
	conn.Write(splash)
	conn.Write(middle)

	io := bufio.NewReader(conn)

	line, _ := io.ReadString('\n')
	name := strings.Replace(strings.Replace(line, "\n", "", -1), "\r", "", -1)
	if name == "" {
		return Player{}, errors.New("Empty name")
	}
	if len(name) > maxNameLength {
		return Player{}, errors.New("Too long name")
	}

	return Player{Conn: conn, Name: name, Health: 100, Car: Car{Speed: 1}}, nil
}

func checkRoundReady(compileRoundChannel, runningRoundChannel chan Round) {
	for {
		fmt.Println("compile/waiting rounds:", len(compileRoundChannel))
		r := <-compileRoundChannel
		fmt.Println(r.Id, "players in round:", len(r.Players))

		if len(r.Players) == maxPlayersPerRound ||
			(r.State == WAITING && r.LastStateChange.Add(maxRoundWaitingTimeSec*time.Second).Before(time.Now())) {
			// We are starting round if it is fully booked or waiting time is expired
			fmt.Println(r.Id, "Round has changed to the state STARTING")
			r.State = STARTING
			r.LastStateChange = time.Now()
			runningRoundChannel <- r
		} else if len(r.Players) >= minPlayersPerRound {
			if r.State == COMPILING {
				fmt.Println(r.Id, "Round has changed to the state WAITING")
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
		round := <-runningRoundChannel
		if len(round.Players) > 0 {
			for len(round.Players) < maxPlayersPerRound {
				p := round.generateBot()
				p.initPlayer(len(round.Players))
				round.Players = append(round.Players, p)
			}
			go round.start()
		}
	}
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

func prepare(conn net.Conn, splash []byte, compileRoundChannel chan Round) {
	p, err := getPlayerData(conn, splash)
	if err != nil {
		conn.Close()
		return
	}

	p.checkBestRoundForPlayer(compileRoundChannel)
}

func main() {
	// Make random unique
	rand.Seed(time.Now().Unix())
	var logFile, acidPath string
	var port, users int

	flag.StringVar(&logFile, "l", "/var/log/race.log", "Log file")
	flag.IntVar(&port, "p", 4242, "Port to listen")
	flag.StringVar(&acidPath, "a", "/Users/leoleovich/go/src/github.com/leoleovich/crashci/artifacts", "Artifacts location")
	flag.Parse()

	logfile, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	conf = Config{log.New(logfile, "", log.Ldate|log.Lmicroseconds|log.Lshortfile), acidPath}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		os.Exit(2)
	}
	defer l.Close()

	// Read sketches
	cars[LEFT], _ = getAcid("carLeft.txt")
	cars[RIGHT], _ = getAcid("carRight.txt")
	cars[UP], _ = getAcid("carUp.txt")
	cars[DOWN], _ = getAcid("carDown.txt")
	splash, _ := getAcid("splash.txt")

	compileRoundChannel := make(chan Round, maxParallelRounds)
	runningRoundChannel := make(chan Round, maxParallelRounds)

	go checkRoundReady(compileRoundChannel, runningRoundChannel)
	go checkRoundRun(runningRoundChannel)

	for {
		conn, err := l.Accept()
		users++
		fmt.Println("In total", users, "users connected")
		if err != nil {
			conf.Log.Println("Failed to accept request", err)
		}

		go prepare(conn, splash, compileRoundChannel)
	}
}
