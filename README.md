# Generator of IR codes for Toshiba air conditioner

Tested and designed for RAS-B10BKVG-E (WH-UA01NE) but 
may work on other models as well.

IR uses Samsung-style protocol. It is possible to use 
[Arduino IRremote library](https://github.com/z3t0/Arduino-IRremote)
but Samsung IR implementation needs to be extended to be able to send
and receive two 32bit uints and one 8bit uint.

Without a modification, IR library returns the first 32bits only
which is `F20D03FC` in most cases.

Complete IR data format
```
first 32bits     second 32bits (payload)  payload checksum
0xAABBCCDD       0xEEEEEEEE               0xFF

AABB = always 0xF20D0
CC = actual command
DD = xor checksum  (AA ^ BB ^ CC)

0xEEEEEEEE = state bits (various parts represent various state)

0xFF = xor checksum of all 4 bytes from 0xEEEEEEEE
```

Example implementation `toshiba.go` generates all three parts.

To run:
```
go run toshiba.go
```

You can change arguments of sendModeFanTemp() call and re-run it
to see a different triple generated.

## Contribution
You are welcome to test it on your Toshiba AC and report your
findings back.

## License
MIT

