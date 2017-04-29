package main

type Car struct {
	Borders   Rectangle
	Direction int
	Speed     int64
}

func (rectangle *Rectangle) nextTo(r *Rectangle, symbols int) int {

	for side := 0; side < 4; side++ {
		tmpRect := *rectangle
		if side == LEFT {
			tmpRect.Points[LEFTUP].X -= symbols
			tmpRect.Points[LEFTDOWN].X -= symbols
		} else if side == RIGHT {
			tmpRect.Points[RIGHTUP].X += symbols
			tmpRect.Points[RIGHTDOWN].X += symbols
		} else if side == UP {
			tmpRect.Points[LEFTUP].Y -= symbols
			tmpRect.Points[RIGHTUP].X -= symbols
		} else {
			tmpRect.Points[LEFTDOWN].Y += symbols
			tmpRect.Points[RIGHTDOWN].X += symbols
		}
		if tmpRect.intersects(r) {
			return side
		}
	}
	return -1
}

func (car *Car) recalculateBorders(crash bool) {
	switch car.Direction {
	case RIGHT:
		for i := range car.Borders.Points {
			if crash {
				car.Borders.Points[i].X--
			} else {
				car.Borders.Points[i].X++
			}
		}
	case LEFT:
		for i, _ := range car.Borders.Points {
			if crash {
				car.Borders.Points[i].X++
			} else {
				car.Borders.Points[i].X--
			}
		}
	case UP:
		for i, _ := range car.Borders.Points {
			if crash {
				car.Borders.Points[i].Y++
			} else {
				car.Borders.Points[i].Y--
			}
		}
	case DOWN:
		for i, _ := range car.Borders.Points {
			if crash {
				car.Borders.Points[i].Y--
			} else {
				car.Borders.Points[i].Y++
			}
		}
	}
}
