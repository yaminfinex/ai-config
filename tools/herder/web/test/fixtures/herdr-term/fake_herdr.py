"""A stand-in herdr client for the herdr view's browser tests.

It turns on SGR mouse tracking as herdr does, says hello, and then reports
what it gets: every size the PTY takes ("size COLS ROWS") and every chunk
of input as hex ("in HEX"), on screen and as lines in the LOG file.
"""
import fcntl
import os
import signal
import struct
import sys
import termios
import tty

log = open(sys.argv[1], 'a', buffering=1)


def say(line):
    log.write(line + '\n')
    os.write(1, (line + '\r\n').encode())


def size(*_):
    rows, cols, _, _ = struct.unpack('HHHH', fcntl.ioctl(0, termios.TIOCGWINSZ, b'\0' * 8))
    say(f'size {cols} {rows}')


tty.setraw(0)
signal.signal(signal.SIGWINCH, size)
os.write(1, b'\x1b[?1000h\x1b[?1006hhello from herdr\r\n')
size()
while True:
    data = os.read(0, 4096)
    if not data:
        break
    say('in ' + data.hex())
