package main

import (
	"math/rand"
	"net"
	"time"
)

type Player struct {
	Conn      net.Conn
	Name      string
	Health    int64
	LastCrash int64
	Color     int
	Bot       bool
	Bombs     int
	DropBomb  bool
	Car       Car
}

func (p *Player) initPlayer(id int) {
	switch id {
	case 0:
		initX, initY := 1, 1
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + horizontalCarWidth - 1, initY},
			{initX + horizontalCarWidth - 1, initY + horizontalCarHeight - 1},
			{initX, initY + horizontalCarHeight - 1}}}
		p.Car.Direction = RIGHT
	case 1:
		initX, initY := mapWidth-nameTableWidth-verticalCarWidth, 1
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + verticalCarWidth - 1, initY},
			{initX + verticalCarWidth, initY + verticalCarHeight - 1},
			{initX, initY + verticalCarHeight - 1}}}
		p.Car.Direction = DOWN
	case 2:
		initX, initY := mapWidth-nameTableWidth-horizontalCarWidth, mapHeight-horizontalCarHeight-1
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + horizontalCarWidth - 1, initY},
			{initX + horizontalCarWidth - 1, initY + horizontalCarHeight - 1},
			{initX, initY + horizontalCarHeight - 1}}}
		p.Car.Direction = LEFT
	case 3:
		initX, initY := 1, mapHeight-verticalCarHeight-1
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + verticalCarWidth - 1, initY},
			{initX + verticalCarWidth - 1, initY + verticalCarHeight - 1},
			{initX, initY + verticalCarHeight - 1}}}
		p.Car.Direction = UP
	case 4:
		initX, initY := (mapWidth-nameTableWidth)/2, mapHeight/2
		p.Car.Borders = Rectangle{[4]Point{
			{initX, initY},
			{initX + verticalCarWidth - 1, initY},
			{initX + verticalCarWidth - 1, initY + verticalCarHeight - 1},
			{initX, initY + verticalCarHeight - 1}}}
		p.Car.Direction = DOWN
	}
	p.Bombs = 1
	p.LastCrash = time.Now().Add(10 * time.Second).Unix()
	// Colors are sequential, so we can use first color RED and set the rest based on IDs
	p.Color = RED + id
}

func (p *Player) checkBestRoundForPlayer(compileRoundChannel chan Round) {
	foundRoundForUser := false
	for i := 0; i < len(compileRoundChannel); i++ {
		select {
		case r := <-compileRoundChannel:
			// If any round is "compiling" now
			if len(r.Players) < maxPlayersPerRound && !p.searchDuplicateName(&r) {
				p.initPlayer(len(r.Players))
				r.Players = append(r.Players, *p)
				compileRoundChannel <- r
				foundRoundForUser = true
				break
			} else {
				compileRoundChannel <- r
			}
		default:
		}
	}

	if !foundRoundForUser {
		// We need a new round
		r := Round{Id: rand.Int(), State: COMPILING, FrameBuffer: make([]Symbol, mapWidth*mapHeight), Bonus: Point{-1, -1}, Bombs: make(map[Point]bool)}
		p.initPlayer(len(r.Players))
		r.Players = append(r.Players, *p)
		compileRoundChannel <- r
	}
}

func (player *Player) searchDuplicateName(round *Round) bool {
	for _, pl := range round.Players {
		if player.Name == pl.Name {
			return true
		}
	}
	return false
}

func (player *Player) readDirection(round *Round) {
	if initTelnet(player.Conn) != nil {
		return
	}

	for {
		if player.Health <= 0 || round.State == FINISHED {
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
			} else if escpos == 0 && direction[0] == 3 {
				// Ctrl+C
				player.Health = 0
				return
			} else if escpos == 0 && direction[0] == 32 {
				// Space
				if player.Bombs > 0 {
					player.DropBomb = true
				}
			} else if escpos == 0 && direction[0] == 27 {
				escpos = 1
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
Checks of the DAMAGE_SIDE is safe
*/
func (player *Player) checkOkSide(players []*Player, side int) bool {
	for _, p := range players {
		if p.Name == player.Name {
			continue
		}
		// If player nextTo someone from the DAMAGE_SIDE - return false
		if side == player.Car.Borders.nextTo(&p.Car.Borders, 3) {
			return false
		}
	}
	return true
}

func (player *Player) navigateBot(myCenter, targetCenter *Point, restPlayers []*Player) {
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
}

func (player *Player) moveBot(round *Round) {
	for {
		if player.Health <= 0 || round.State == FINISHED {
			return
		}
		rid := round.getRandomAliveNonBotPlayerId()
		if rid == -1 {
			continue
		}

		targetPlayer := &round.Players[rid]
		allPlayersExceptMeAndTarget := round.getPlayersExcept([]Player{*targetPlayer, *player})
		allPlayersExceptMe := append(allPlayersExceptMeAndTarget, targetPlayer)

		heartRect := &Rectangle{Points: [4]Point{
			{round.Bonus.X, round.Bonus.Y},
			{round.Bonus.X, round.Bonus.Y},
			{round.Bonus.X, round.Bonus.Y},
			{round.Bonus.X, round.Bonus.Y}},
		}
		targetCenter := &Point{}
		for i := 0; i < 20; i++ {
			if round.State == FINISHED || player.Health <= 0 {
				return
			} else if round.State == RUNNING {
				// Check if BOT is throwing the bomb
				if player.Bombs > 0 && rand.Int()%highFactor == 0 {
					player.DropBomb = true
				}

				// Do not hunt dead player
				if targetPlayer.Health <= 0 {
					break
				}
				if player.Health <= 0 {
					return
				}

				// Navigate Bot based on centers of cars
				myCenter := &Point{
					player.Car.Borders.Points[LEFTUP].X + (player.Car.Borders.Points[RIGHTUP].X-player.Car.Borders.Points[LEFTUP].X)/2,
					player.Car.Borders.Points[LEFTUP].Y + (player.Car.Borders.Points[LEFTDOWN].Y-player.Car.Borders.Points[LEFTUP].Y)/2,
				}

				// Check if heart next to Bot
				heartOnSide := player.Car.Borders.nextTo(heartRect, 5)
				// Bot prefers to grab the heart
				if heartOnSide != -1 && round.Bonus.X != -1 && round.Bonus.Y != -1 {
					targetCenter = &Point{round.Bonus.X, round.Bonus.Y}
				} else {
					targetCenter = &Point{
						targetPlayer.Car.Borders.Points[LEFTUP].X + (targetPlayer.Car.Borders.Points[RIGHTUP].X-targetPlayer.Car.Borders.Points[LEFTUP].X)/2,
						targetPlayer.Car.Borders.Points[LEFTUP].Y + (targetPlayer.Car.Borders.Points[LEFTDOWN].Y-targetPlayer.Car.Borders.Points[LEFTUP].Y)/2,
					}
				}

				player.navigateBot(myCenter, targetCenter, allPlayersExceptMe)

			}
			time.Sleep(time.Duration(500 * time.Millisecond))
		}
	}
}

func (player *Player) checkHitWall() bool {
	for _, point := range player.Car.Borders.Points {
		if point.X < 1 || point.X > mapWidth-nameTableWidth || point.Y < 1 || point.Y > mapHeight-1 {
			player.Health -= DAMAGE_FRONT * player.Car.Speed
			return true
		}
	}
	return false
}

func (player *Player) checkHitAnotherCar(round *Round) bool {
	for _, opponent := range round.Players {
		if player.Name == opponent.Name {
			continue
		}

		if player.Car.Borders.intersects(&opponent.Car.Borders) {
			switch player.Car.Borders.nextTo(&opponent.Car.Borders, 0) {
			case LEFT:
				// Player was hit from LEFT
				switch player.Car.Direction {
				case LEFT:
					// DAMAGE_FRONT crash
					player.Health -= DAMAGE_FRONT * player.Car.Speed
				case RIGHT:
					// DAMAGE_BACK crash
					player.Health -= DAMAGE_BACK * (maxSpeed - player.Car.Speed)
				case UP | DOWN:
					// DAMAGE_SIDE crash
					player.Health -= DAMAGE_SIDE
				}
			case RIGHT:
				// Player was hit from RIGHT
				switch player.Car.Direction {
				case RIGHT:
					// DAMAGE_FRONT crash
					player.Health -= DAMAGE_FRONT * player.Car.Speed
				case LEFT:
					// DAMAGE_BACK crash
					player.Health -= DAMAGE_BACK * (maxSpeed - player.Car.Speed)
				case UP | DOWN:
					// DAMAGE_SIDE crash
					player.Health -= DAMAGE_SIDE
				}
			case UP:
				// Player was hit from UP
				switch player.Car.Direction {
				case UP:
					// DAMAGE_FRONT crash
					player.Health -= DAMAGE_FRONT * player.Car.Speed
				case DOWN:
					// DAMAGE_BACK crash
					player.Health -= DAMAGE_BACK * (maxSpeed - player.Car.Speed)
				case LEFT | RIGHT:
					// DAMAGE_SIDE crash
					player.Health -= DAMAGE_SIDE
				}
			case DOWN:
				// Player was hit from DOWN
				switch player.Car.Direction {
				case DOWN:
					// DAMAGE_FRONT crash
					player.Health -= DAMAGE_FRONT * player.Car.Speed
				case UP:
					// DAMAGE_BACK crash
					player.Health -= DAMAGE_BACK * (maxSpeed - player.Car.Speed)
				case LEFT | RIGHT:
					// DAMAGE_SIDE crash
					player.Health -= DAMAGE_SIDE
				}
			}

			// Hit in the back
			if (player.Car.Direction == RIGHT && opponent.Car.Direction == LEFT) ||
				(player.Car.Direction == LEFT && opponent.Car.Direction == RIGHT) ||
				(player.Car.Direction == UP && opponent.Car.Direction == DOWN) ||
				(player.Car.Direction == DOWN && opponent.Car.Direction == UP) {
				// Face to face
				player.Health -= DAMAGE_FRONT * player.Car.Speed
			} else if (player.Car.Direction == RIGHT && opponent.Car.Direction == UP) ||
				(player.Car.Direction == LEFT && opponent.Car.Direction == UP) ||
				(player.Car.Direction == RIGHT && opponent.Car.Direction == DOWN) ||
				(player.Car.Direction == LEFT && opponent.Car.Direction == DOWN) ||
				(player.Car.Direction == UP && opponent.Car.Direction == RIGHT) ||
				(player.Car.Direction == UP && opponent.Car.Direction == LEFT) ||
				(player.Car.Direction == DOWN && opponent.Car.Direction == RIGHT) ||
				(player.Car.Direction == DOWN && opponent.Car.Direction == LEFT) {
				// Side hit
				player.Health -= DAMAGE_SIDE * player.Car.Speed
			} else {
				// Back hit
				player.Health -= DAMAGE_BACK * (player.Car.Speed - opponent.Car.Speed)
			}
			return true
		}
	}

	return false
}

func (player *Player) checkHit(round *Round) {
	if player.checkHitWall() || player.checkHitAnotherCar(round) {
		player.Car.recalculateBorders(true)
		player.LastCrash = time.Now().Unix()

		// Bounce player to the opposite direction
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
	}
}

func (player *Player) checkHitBonus(round *Round) {
	bonusRect := &Rectangle{Points: [4]Point{
		{round.Bonus.X, round.Bonus.Y},
		{round.Bonus.X, round.Bonus.Y},
		{round.Bonus.X, round.Bonus.Y},
		{round.Bonus.X, round.Bonus.Y}},
	}
	if player.Car.Borders.intersects(bonusRect) {
		player.Health += bonusPoint
		player.Car.Speed = maxSpeed
		round.Bonus.X, round.Bonus.Y = -1, -1
	}
}

func (player *Player) checkHitBomb(round *Round) {
	round.Lock()
	for bomb := range round.Bombs {
		bombRect := &Rectangle{Points: [4]Point{
			{bomb.X, bomb.Y},
			{bomb.X, bomb.Y},
			{bomb.X, bomb.Y},
			{bomb.X, bomb.Y}},
		}

		if player.Car.Borders.intersects(bombRect) {
			player.Health -= bonusPoint
			player.LastCrash = time.Now().Unix()
			player.Car.Speed = 1
			delete(round.Bombs, bomb)
		}
	}
	round.Unlock()
}

func (player *Player) checkPosition(round *Round) {
	for {
		if player.Health <= 0 || round.State == FINISHED {
			return
		} else if round.State == RUNNING {
			// Move player
			player.Car.recalculateBorders(false)

			// Check if we catch the bonus
			player.checkHitBonus(round)

			// Check if we hit the bomb
			player.checkHitBomb(round)

			// Check if we hit something
			player.checkHit(round)
		}

		/*
		 Because vertical symbols are 3x bigger, than horizontal, we need to slowdown recalculation of vertical objects
		*/
		slowerDown := int64(1)
		if player.Car.Direction == UP || player.Car.Direction == DOWN {
			slowerDown = 3
		}

		time.Sleep(time.Duration(slowerDown*150/player.Car.Speed) * time.Millisecond)
	}
}

func (player *Player) checkSpeed(round *Round) {
	for {
		if player.Health <= 0 || round.State == FINISHED {
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

func (player *Player) checkHealth(round *Round) {
	for {
		if player.Health <= 0 {
			player.Health = 0
			return
		}

		for num, player := range round.Players {
			if player.Health > 100 {
				round.Players[num].Health = 100
			} else if player.Health <= 0 {
				round.Players[num].Health = 0
				round.Players[num].Color = BOLD
			}
		}
		time.Sleep(1 % framesPerSecond * 100 * time.Millisecond)
	}
}

func (player *Player) checkBomb(round *Round) {
	for {
		if player.Health <= 0 || round.State == FINISHED {
			return
		}

		for num, player := range round.Players {
			if player.DropBomb {
				bombPosition := Point{}

				switch player.Car.Direction {
				case LEFT:
					bombPosition.X = player.Car.Borders.Points[RIGHTUP].X + 1
					bombPosition.Y = player.Car.Borders.Points[RIGHTUP].Y + (player.Car.Borders.Points[RIGHTDOWN].Y-player.Car.Borders.Points[RIGHTUP].Y)/2
				case RIGHT:
					bombPosition.X = player.Car.Borders.Points[LEFTUP].X - 1
					bombPosition.Y = player.Car.Borders.Points[RIGHTUP].Y + (player.Car.Borders.Points[RIGHTDOWN].Y-player.Car.Borders.Points[RIGHTUP].Y)/2
				case UP:
					bombPosition.X = player.Car.Borders.Points[LEFTUP].X + (player.Car.Borders.Points[RIGHTUP].X-player.Car.Borders.Points[LEFTUP].X)/2
					bombPosition.Y = player.Car.Borders.Points[LEFTDOWN].Y + 1
				case DOWN:
					bombPosition.X = player.Car.Borders.Points[LEFTUP].X + (player.Car.Borders.Points[RIGHTUP].X-player.Car.Borders.Points[LEFTUP].X)/2
					bombPosition.Y = player.Car.Borders.Points[LEFTUP].Y - 1
				}
				if bombPosition.X > 1 && bombPosition.X < mapWidth-nameTableWidth-1 && bombPosition.Y > 1 && bombPosition.Y < mapHeight-1 {
					round.Players[num].DropBomb = false
					round.Players[num].Bombs--
					round.Lock()
					round.Bombs[bombPosition] = true
					round.Unlock()
				}
			} else if rand.Int()%(highFactor*lowFactor) == 0 && round.State == RUNNING {
				round.Players[num].Bombs++
			}
		}

		time.Sleep(1 % framesPerSecond * 100 * time.Millisecond)
	}
}

func (player *Player) writeToThePlayer(message []byte, clean bool) {
	if clean {
		_, err := player.Conn.Write(clear)
		if err != nil {
			// Kick user if connection got lost
			player.Health = 0
			return
		}
	}
	_, err := player.Conn.Write(home)
	if err != nil {
		player.Health = 0
		return
	}
	_, err = player.Conn.Write(message)
	if err != nil {
		player.Health = 0
		return
	}
}
