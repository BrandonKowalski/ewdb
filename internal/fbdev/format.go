package fbdev

import "ewdb/internal/capture"

type Field struct {
	Offset, Length uint32
}

func Format(bpp uint32, red, green, blue, transp Field) uint32 {
	switch bpp {
	case 32:
		alpha := transp.Length == 8 && transp.Offset == 24
		switch {
		case red.Offset == 16 && green.Offset == 8 && blue.Offset == 0 && red.Length == 8:
			if alpha {
				return capture.FourCC('A', 'R', '2', '4')
			}
			return capture.FourCC('X', 'R', '2', '4')
		case red.Offset == 0 && green.Offset == 8 && blue.Offset == 16 && red.Length == 8:
			if alpha {
				return capture.FourCC('A', 'B', '2', '4')
			}
			return capture.FourCC('X', 'B', '2', '4')
		}
	case 24:
		switch {
		case red.Offset == 16 && green.Offset == 8 && blue.Offset == 0:
			return capture.FourCC('R', 'G', '2', '4')
		case red.Offset == 0 && green.Offset == 8 && blue.Offset == 16:
			return capture.FourCC('B', 'G', '2', '4')
		}
	case 16:
		if red.Length == 5 && green.Length == 6 && blue.Length == 5 {
			switch {
			case red.Offset == 11 && blue.Offset == 0:
				return capture.FourCC('R', 'G', '1', '6')
			case red.Offset == 0 && blue.Offset == 11:
				return capture.FourCC('B', 'G', '1', '6')
			}
		}
	}
	return 0
}
