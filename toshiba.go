// Toshiba A/C IR command generator
// Copyright (C) 2018 K3A.me
// License MIT

package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/alexflint/go-arg"
)

// AuthMiddleware is a middleware function that checks for Basic Auth or Bearer Token
func AuthMiddleware(password string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		if password != "" && strings.HasPrefix(authHeader, "Basic ") {
			// Handle Basic Auth
			payload := strings.TrimPrefix(authHeader, "Basic ")
			decoded, err := decodeBase64(payload)
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			// The decoded string is in the format "username:password"
			parts := strings.SplitN(decoded, ":", 2)
			if len(parts) != 2 || parts[1] != password {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		} else if password != "" && strings.HasPrefix(authHeader, "Bearer ") {
			// Handle Bearer Token
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token != password {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		} else {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// If we reach here, authentication was successful
		next.ServeHTTP(w, r)
	}
}

// decodeBase64 decodes a base64 encoded string
func decodeBase64(encoded string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

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
	log.Printf("0x%08X 0x%08X 0x%02X\n", prefix, state&0xFFFFFFFF, endByte&0xFF)

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

var eco bool

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

	pulses := ""
	var mode modeType
	specMode := NoSpecialMode
	var f *os.File
	var cmd *exec.Cmd

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		goto errRes
	}

	mode, err = parseMode(req.Mode)
	if err != nil {
		goto errRes
	}

	if req.HiPower {
		specMode = HiPowerSpecialMode
	} else if eco {
		specMode = EcoSpecialMode
	}

	pulses, err = makeModeFanTemp(unitType(req.Unit), mode, specMode, fanType(req.Fan), uint32(req.Temperature))
	if err != nil {
		goto errRes
	}

	f, err = os.CreateTemp("", "toshiba-ac")
	if err != nil {
		goto errRes
	}
	defer f.Close()
	defer os.Remove(f.Name())

	_, err = f.WriteString(pulses)
	if err != nil {
		goto errRes
	}

	cmd = exec.Command("ir-ctl", "-s", f.Name())
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err = cmd.Run()
	if err != nil {
		err = fmt.Errorf("error running ir-ctl -s %s: %v", f.Name(), err)
	}

errRes:
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		log.Printf("ERROR: %v\n", err)
		return
	}
}

type FanTempCmd struct {
	Mode string `arg:"positional,required"`
	Temp int    `arg:"positional,required"`
}

type ServeCmd struct {
	Host string `arg:"--host" default:"127.0.0.1"`
	Port int    `arg:"--port" default:"1958"`
	Auth string `arg:"--auth"`
}

type FixCmd struct {
}

type SwingCmd struct {
}

var args struct {
	FanTemp *FanTempCmd `arg:"subcommand:fantemp"`
	Serve   *ServeCmd   `arg:"subcommand:serve"`
	Fix     *FixCmd     `arg:"subcommand:fix"`
	Swing   *SwingCmd   `arg:"subcommand:swing"`
	Unit    int         `arg:"--unit" default:"0"`
	HiPower bool        `arg:"--hipower"`
	Eco     bool        `arg:"--eco"`
	Fan     int         `arg:"--fan" default:"0"`
}

func main() {
	arg.MustParse(&args)

	var err error

	switch {
	case args.Fix != nil:
		_, err = makeFix(unitType(args.Unit))
	case args.Swing != nil:
		_, err = makeSwing(unitType(args.Unit))
	case args.Serve != nil:
		eco = args.Eco
		http.HandleFunc("/set", AuthMiddleware(args.Serve.Auth, handleSet))
		listen := fmt.Sprintf("%s:%d", args.Serve.Host, args.Serve.Port)
		log.Printf("listening on %s\n", listen)
		err := http.ListenAndServe(listen, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			return
		}
	case args.FanTemp != nil:
		var mode modeType
		mode, err = parseMode(args.FanTemp.Mode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			return
		}

		var specMode = NoSpecialMode
		if args.HiPower {
			specMode = HiPowerSpecialMode
		} else if args.Eco {
			specMode = EcoSpecialMode
		}

		_, err = makeModeFanTemp(
			unitType(args.Unit),
			mode,
			specMode,
			fanType(args.Fan),
			uint32(args.FanTemp.Temp),
		)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		return
	}
}

