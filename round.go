package main

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

type Round struct {
	Players         []Player
	State           int
	LastStateChange time.Time
	Bonus           Point
	Bombs           map[Point]bool
	FrameBuffer     Symbols
	Lock            sync.Mutex
}

func (round *Round) generateMap() {
	// http://www.theasciicode.com.ar
	for row := 0; row < mapHeight; row++ {
		for column := 0; column < mapWidth; column++ {
			var char []byte
			if (row == 0 || row == mapHeight-1) && column < mapWidth-2 {
				char = []byte("─")
			} else if column == 0 || column == mapWidth-3 || column == mapWidth-nameTableWidth {
				char = []byte("│")
			} else if column == mapWidth-2 {
				char = []byte("\r")
			} else if column == mapWidth-1 {
				char = []byte("\n")
			} else {
				char = []byte(" ")
			}
			round.FrameBuffer[row*mapWidth+column] = Symbol{0, char}
		}
	}
}

func (round *Round) generateBot() Player {
	// Get data of player and return the structure

	for {
		p := Player{Name: fmt.Sprintf("Bot %d", rand.Intn(10)+1), Health: 100, Bot: true, Car: Car{Speed: 1}}
		if !p.searchDuplicateName(round) {
			return p
		}
	}
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

func (round *Round) gameLogic() {
	for i := range round.Players {
		if round.Players[i].Bot {
			go round.Players[i].moveBot(round)
		} else {
			go round.Players[i].readDirection(round)
		}
		go round.Players[i].checkPosition(round)
		go round.Players[i].checkSpeed(round)
		go round.Players[i].checkHealth(round)
		go round.Players[i].checkBomb(round)
	}
}

func (round *Round) checkGameOver(activeFrameBuffer Symbols) {
	humans := 0
	deadHumans := 0
	deadPlayers := 0
	winnersName := ""

	for _, p := range round.Players {
		if p.Health <= 0 {
			deadPlayers++
		}

		if !p.Bot {
			humans++
			if p.Health <= 0 {
				deadHumans++
			} else {
				winnersName = p.Name
			}
		}
	}

	secondsLeft := round.LastStateChange.Unix() + maxRoundRunningTimeSec - time.Now().Unix()
	if humans == deadHumans || maxPlayersPerRound-deadPlayers == 1 || secondsLeft <= 0 {
		round.State = FINISHED
		fmt.Println("Round has changed to the state FINISHED")
		if maxPlayersPerRound-deadPlayers == 1 {
			winnerStr := "THE WINNER IS " + winnersName + "!!!"
			for i, char := range []byte(winnerStr) {
				activeFrameBuffer[mapWidth*(mapHeight/2-2)+mapWidth/2-len(winnerStr)/2+i] = Symbol{GREEN, []byte{char}}
			}
			round.writeToAllPlayers(activeFrameBuffer.symbolsToByte(), false)
			time.Sleep(5 * time.Second)
		}
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

func (round *Round) applyNames(lineBetweenPlayersInBar int) {
	for line, player := range round.Players {
		for i, char := range []byte(player.Name) {
			round.FrameBuffer[(line*lineBetweenPlayersInBar+1)*mapWidth+(mapWidth-nameTableWidth+1)+i] = Symbol{player.Color, []byte{char}}
		}
		round.FrameBuffer[(line*lineBetweenPlayersInBar+1)*mapWidth+(mapWidth-nameTableWidth+1)+len(player.Name)] = Symbol{RESET, []byte{':'}}
	}
}

func (round *Round) applyBonus(activeFrameBuffer []Symbol) {
	if round.State == STARTING {
		return
	}
	if round.Bonus.X == -1 && round.Bonus.Y == -1 {
		if rand.Int()%lowFactor == 0 {
			round.Bonus = Point{rand.Intn(mapWidth-nameTableWidth-2) + 1, rand.Intn(mapHeight-2) + 1}
		}
	} else {
		activeFrameBuffer[round.Bonus.Y*mapWidth+round.Bonus.X] = Symbol{RED, []byte(bonus)}
	}
}

func (round *Round) applyBombs(activeFrameBuffer []Symbol, lineBetweenPlayersInBar int) {
	if round.State == STARTING {
		return
	}

	round.Lock.Lock()
	for b, _ := range round.Bombs {
		activeFrameBuffer[b.Y*mapWidth+b.X] = Symbol{BOLD, []byte(bomb)}
	}
	round.Lock.Unlock()
}

func (round *Round) applyUserData(activeFrameBuffer []Symbol, lineBetweenPlayersInBar int) {
	for num, player := range round.Players {
		// Apply health
		health := []byte(fmt.Sprintf("Health: %3d", player.Health))
		for i, char := range health {
			// +1 because health is next line after the name
			activeFrameBuffer[((num*lineBetweenPlayersInBar+1)+1)*mapWidth+(mapWidth-3)-len(health)+i] = Symbol{player.Color, []byte{char}}
		}

		// Apply the amount of bombs to the bar
		bombs := []byte(fmt.Sprintf("Bombs: %4d", round.Players[num].Bombs))
		for i, char := range bombs {
			// +2 because "bombs" is next line after the name
			activeFrameBuffer[((num*lineBetweenPlayersInBar+2)+1)*mapWidth+(mapWidth-3)-len(bombs)+i] = Symbol{player.Color, []byte{char}}
		}
	}
}

func (round *Round) applyGetReady(activeFrameBuffer []Symbol, getReadyCounter *int) {
	if round.State == STARTING {
		getReady := "GET READY!"
		if *getReadyCounter == 0 {
			fmt.Println("Round has changed to the state RUNNING")
			round.State = RUNNING
		} else if *getReadyCounter <= framesPerSecond*1 {
			getReady += " 1"
		} else if *getReadyCounter <= framesPerSecond*2 {
			getReady += " 2"
		} else if *getReadyCounter <= framesPerSecond*3 {
			getReady += " 3"
		}

		for i, char := range []byte(getReady) {
			activeFrameBuffer[mapWidth*(mapHeight/2-2)+mapWidth/2-len(getReady)/2+i] = Symbol{GREEN, []byte{char}}
		}
		*getReadyCounter--
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

func (round Round) start() {
	lineBetweenPlayersInBar := mapHeight / len(round.Players)
	getReadyCounter := getReadyPause / framesPerSecond

	round.gameLogic()
	round.generateMap()
	round.applyNames(lineBetweenPlayersInBar)

	for {
		activeFrameBuffer := make(Symbols, len(round.FrameBuffer))
		copy(activeFrameBuffer, round.FrameBuffer)

		round.applyUserData(activeFrameBuffer, lineBetweenPlayersInBar)
		round.applyBonus(activeFrameBuffer)
		round.applyBombs(activeFrameBuffer, lineBetweenPlayersInBar)
		round.applyCars(activeFrameBuffer)
		round.applyGetReady(activeFrameBuffer, &getReadyCounter)

		round.writeToAllPlayers(activeFrameBuffer.symbolsToByte(), false)

		round.checkGameOver(activeFrameBuffer)
		if round.State == FINISHED {
			round.over()
			return
		}
		time.Sleep(1 % framesPerSecond * 100 * time.Millisecond)
	}

}
