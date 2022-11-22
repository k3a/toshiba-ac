// Toshiba A/C IR command generator
// Copyright (C) 2018 K3A.me
// License MIT

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
)

func makeBit(val bool) string {
	if val {
		return "+591 -1643 " // one
	} else {
		return "+591 -591 " // zero
	}
}

func makeCmdRaw(cmd uint32, state uint32, endByte uint8) (string, error) {
	prefix := uint32(0xF20D0000)

	if cmd > 0xFF {
		return "", fmt.Errorf("sendCmd: cmd must be in range [0, 0xFF]")
	}

	// command
	prefix |= ((cmd & 0xFF) << 8)

	// checksum
	prefix |= ((prefix >> 24) & 0xFF) ^ ((prefix >> 16) & 0xFF) ^ ((prefix >> 8) & 0xFF)

	// print command parts
	fmt.Printf("0x%08X 0x%08X 0x%02X\n", prefix, state&0xFFFFFFFF, endByte&0xFF)

	// construct pulse output
	out := "+4496 -4414 " // header
	for i := 31; i >= 0; i-- {
		out += makeBit(prefix&(1<<i) != 0)
	}
	for i := 31; i >= 0; i-- {
		out += makeBit(state&(1<<i) != 0)
	}
	for i := 7; i >= 0; i-- {
		out += makeBit(endByte&(1<<i) != 0)
	}
	out += "+600 " // tail

	return out, nil
}

type unitType uint8

const (
	UnitA = unitType(0)
	UnitB = unitType(1)
)

type modeType uint8

const (
	AutoMode    = modeType(0)
	CoolingMode = modeType(1)
	DryingMode  = modeType(2)
	HeatingMode = modeType(3)
	PwrOffMode  = modeType(7)
)

type fanType uint8

const (
	FanAuto = fanType(0)
	Fan1    = fanType(2)
	Fan2    = fanType(3)
	Fan3    = fanType(4)
	Fan4    = fanType(5)
	Fan5    = fanType(6)
)

type cmdType uint8

const (
	CheckFixSwingCommand = cmdType(1)
	ModeFanTempCommand   = cmdType(3)
	HiPowerEcoCommand    = cmdType(4)
)

type specialModeType uint8

const (
	NoSpecialMode      = specialModeType(0)
	HiPowerSpecialMode = specialModeType(1)
	EcoSpecialMode     = specialModeType(3)
)

const NoChecksum = uint8(0x60)

func makeModeFanTemp(unit unitType, mode modeType, specialMode specialModeType, fan fanType, tempCelsius uint32) (string, error) {
	//          abcd efgh ijkl mnop qrst uvwx yz23 4567     checksum - dec
	//23 deg    0000 0001 0110 0000 0000 0001 0000 0000     0110 0000 - 96
	//                    ijkl - temp above 17 (so 0000 is 17)
	//                                    vwx – 000:auto, 001:cooling, 010:drying, 011:heating, 111:pwroff
	//               efgh - special mode bits (1 - normal mode, 9 - hipower or eco depending on endByte)

	specialModeBits := uint32(1)
	if specialMode != NoSpecialMode {
		specialModeBits = 9
	}
	state := uint32(specialModeBits << 24) // to set "h" bit to 1 or efgh to 9 (special mode)

	if tempCelsius < 17 || tempCelsius > 30 {
		return "", fmt.Errorf("sendCmd: temperature must be in the range [17, 30]")
	}

	state |= ((uint32(tempCelsius) - 17) & 0xF) << 20 // 4 bits of temp over 17
	state |= (uint32(fan) & 0x1F) << 13               // 3 bits of fan type
	state |= uint32(mode&0x1F) << 8                   // 3 bits of mode type

	var endByte uint8
	if specialMode != NoSpecialMode {
		endByte = uint8(specialMode)
	} else {
		// common state checksum
		endByte = uint8(((state >> 24) & 0xFF) ^
			((state >> 16) & 0xFF) ^
			((state >> 8) & 0xFF) ^
			(state & 0xFF))
	}

	// first 4bits of command are unit index
	return makeCmdRaw((uint32(unit)<<4)|uint32(ModeFanTempCommand), state, endByte)
}

func makeFix(unit unitType) (string, error) {
	state := uint32(0x21002100)
	return makeCmdRaw((uint32(unit)<<4)|uint32(CheckFixSwingCommand), state, NoChecksum)
}

func makeSwing(unit unitType) (string, error) {
	state := uint32(0x21042500)
	return makeCmdRaw((uint32(unit)<<4)|uint32(CheckFixSwingCommand), state, NoChecksum)
}

func parseMode(inmode string) (modeType, error) {
	mode := AutoMode
	switch inmode {
	case "auto":
		mode = AutoMode
	case "cooling":
		mode = CoolingMode
	case "drying":
		mode = DryingMode
	case "heating":
		mode = HeatingMode
	case "poweroff":
		mode = PwrOffMode
	default:
		return mode, fmt.Errorf("unknown mode %s", inmode)
	}

	return mode, nil
}

func handleSet(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if r.Method != "POST" {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Unit        int    `json:"unit"`
		Mode        string `json:"mode"`
		Temperature int    `json:"temperature"`
		Fan         int    `json:"fan"`
		HiPower     bool   `json:"hiPower"`
		Eco         bool   `json:"eco"`
	}

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mode, err := parseMode(req.Mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	specMode := NoSpecialMode
	if req.HiPower {
		specMode = HiPowerSpecialMode
	} else if *eco {
		specMode = EcoSpecialMode
	}

	var f *os.File

	pulses, err := makeModeFanTemp(unitType(req.Unit), mode, specMode, fanType(req.Fan), uint32(req.Temperature))
	if err != nil {
		goto errRes
	}

	f, err = ioutil.TempFile("", "toshiba-ac")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close()
	defer os.Remove(f.Name())

	_, err = f.WriteString(pulses)
	if err != nil {
		goto errRes
	}

	err = exec.Command("ir-ctl", "-s", f.Name()).Run()

errRes:
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
}

var (
	unit    = flag.Int("unit", 0, "unit to address (0-A, 1-B)")
	hipower = flag.Bool("hipower", false, "Enable Hi-Power mode")
	eco     = flag.Bool("eco", false, "Enable Eco mode")
	fan     = flag.Int("fan", 0, "Set fan speed (0=auto)")
	host    = flag.String("host", "127.0.0.1", "IP to listen on")
	port    = flag.Int("port", 1958, "Port to listen on")
)

func usage(cmd string) {
	if cmd == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s <serve|fantemp|fix|swing> [--unit NUM] ...\n", os.Args[0])
	} else if cmd == "fantemp" {
		fmt.Fprintf(os.Stderr, "Usage: %s fantemp <auto|cooling|drying|heating|poweroff> <TEMP-DEG> [-unit NUM] [-fan FAN] [-hipower|-eco]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       FAN is 0 for Auto or a number between 2 and 6 inclusive\n")
	} else if cmd == "serve" {
		fmt.Fprintf(os.Stderr, "Usage: %s serve [-listen IP] [-port PORT]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "       FAN is 0 for Auto or a number between 2 and 6 inclusive\n")
	}
	os.Exit(1)
}

func main() {
	flag.Parse()

	if len(os.Args) < 2 {
		usage("")
		return
	}

	var err error

	if os.Args[1] == "fix" {
		_, err = makeFix(unitType(*unit))
	} else if os.Args[1] == "swing" {
		_, err = makeSwing(unitType(*unit))
	} else if os.Args[1] == "serve" {
		http.HandleFunc("/set", handleSet)
		listen := fmt.Sprintf("%s:%d", *host, *port)
		fmt.Fprintf(os.Stdout, "listening on %s\n", listen)
		err := http.ListenAndServe(listen, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			return
		}

	} else {
		if len(os.Args) < 4 {
			usage(os.Args[1])
			return
		}

		mode, err := parseMode(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			return
		}

		var specMode = NoSpecialMode
		if *hipower {
			specMode = HiPowerSpecialMode
		} else if *eco {
			specMode = EcoSpecialMode
		}

		temp, err := strconv.Atoi(os.Args[3])
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			return
		}

		_, err = makeModeFanTemp(unitType(*unit), mode, specMode, fanType(*fan), uint32(temp))
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return
	}
}
