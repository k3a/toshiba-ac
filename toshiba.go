// Toshiba A/C IR command generator
// Copyright (C) 2018 K3A.me
// License MIT

package main

import "fmt"

func sendCmdRaw(cmd uint32, state uint32, endByte uint8) error {
	prefix := uint32(0xF20D0000);

	if cmd > 0xFF {
		return fmt.Errorf("sendCmd: cmd must be in range [0, 0xFF]")
	}

	// command
	prefix |= (cmd << 8)

	// checksum
	prefix |= ((prefix>>24) & 0xFF) ^ ((prefix>>16) & 0xFF) ^ ((prefix>>8) & 0xFF)

	fmt.Printf("0x%X 0x%X 0x%X\n", prefix, state, endByte)

	return nil
}

type unitType uint8
const (
    UnitA = unitType(0)
    UnitB = unitType(1)
)

type modeType uint8
const (
	AutoMode = modeType(0)
	CoolingMode = modeType(1)
	DryingMode = modeType(2)
	HeatingMode = modeType(3)
	PwrOffMode = modeType(7)
)

type fanType uint8
const (
	FanAuto = fanType(0)
	Fan1 = fanType(2)
	Fan2 = fanType(3)
	Fan3 = fanType(4)
	Fan4 = fanType(5)
	Fan5 = fanType(6)
)

type cmdType uint8
const (
	CheckFixSwingCommand = cmdType(1)
	ModeFanTempCommand = cmdType(3)
	HiPowerEcoCommand = cmdType(4)
)

type specialModeType uint8
const (
	NoSpecialMode = specialModeType(0)
	HiPowerSpecialMode = specialModeType(1)
	EcoSpecialMode = specialModeType(3)
)

const NoChecksum = uint8(0x60)

func sendModeFanTemp(unit unitType, mode modeType, specialMode specialModeType, fan fanType, tempCelsius uint32) error {
//			abcd efgh ijkl mnop qrst uvwx yz23 4567		checksum - dec
//23 deg	0000 0001 0110 0000 0000 0001 0000 0000		0110 0000 - 96
//                    ijkl - temp above 17 (so 0000 is 17)
//                                    vwx – 000:auto, 001:cooling, 010:drying, 011:heating, 111:pwroff
//               efgh - special mode bits (1 - normal mode, 9 - hipower or eco depending on endByte)
//

	specialModeBits := uint32(1)
	if specialMode != NoSpecialMode {
		specialModeBits = 9
	}
	state := uint32(specialModeBits<<24) // to set "h" bit to 1 or efgh to 9 (special mode)

	if tempCelsius < 17 || tempCelsius > 30 {
		return fmt.Errorf("sendCmd: tempCelsius must be in the range [17, 30]")
	}

	state |= ((uint32(tempCelsius)-17)&0xF)<<20 // 4 bits of temp over 17
	state |= (uint32(fan)&0x1F)<<13 // 3 bits of fan type
	state |= uint32(mode&0x1F)<<8 // 3 bits of mode type

	var endByte uint8
	if specialMode != NoSpecialMode {
		endByte = uint8(specialMode)
	} else {
		// common state checksum
		endByte = uint8(((state>>24) & 0xFF) ^
				((state>>16) & 0xFF) ^
				((state>>8) & 0xFF) ^
				(state & 0xFF))
	}

	// first 4bits of command are unit index
	return sendCmdRaw((uint32(unit)<<4) | uint32(ModeFanTempCommand), state, endByte)
}

func sendFix(unit unitType) error {
	state := uint32(0x21002100)
	return sendCmdRaw((uint32(unit)<<4) | uint32(CheckFixSwingCommand), state, NoChecksum)
}

func sendSwing(unit unitType) error {
	state := uint32(0x21042500)
	return sendCmdRaw((uint32(unit)<<4) | uint32(CheckFixSwingCommand), state, NoChecksum)
}

func main() {
	sendModeFanTemp(UnitA, PwrOffMode, NoSpecialMode, FanAuto, 23)
}
