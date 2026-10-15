"""Run `sh -c COMMAND` in a PTY of COLS x ROWS, as the serve's herdr view does.

stdin goes to the PTY and the PTY's output comes out on stdout. Each
"COLS ROWS" line on fd 3 resizes the PTY (the client gets SIGWINCH). Exits
with the client's status once the PTY closes.
"""
import fcntl
import os
import select
import struct
import subprocess
import sys
import termios


def resize(fd, cols, rows):
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack('HHHH', rows, cols, 0, 0))


def write_all(fd, data):
    while data:
        data = data[os.write(fd, data):]


def main():
    cols, rows, command = int(sys.argv[1]), int(sys.argv[2]), sys.argv[3]
    master, slave = os.openpty()
    resize(master, cols, rows)
    env = {key: value for key, value in os.environ.items() if not key.startswith(('TERM', 'COLUMNS', 'LINES'))}
    env.update(TERM='xterm-256color', COLORTERM='truecolor')
    child = subprocess.Popen(
        ['/bin/sh', '-c', command], stdin=slave, stdout=slave, stderr=slave, env=env,
        start_new_session=True, preexec_fn=lambda: fcntl.ioctl(0, termios.TIOCSCTTY, 0))
    os.close(slave)
    inputs = [0, master, 3]
    control = b''
    while True:
        ready, _, _ = select.select(inputs, [], [])
        if master in ready:
            try:
                data = os.read(master, 32 << 10)
            except OSError:
                data = b''
            if not data:
                break
            write_all(1, data)
        if 0 in ready:
            data = os.read(0, 32 << 10)
            if data:
                write_all(master, data)
            else:
                inputs.remove(0)
        if 3 in ready:
            data = os.read(3, 4096)
            if not data:
                inputs.remove(3)
            control += data
            while b'\n' in control:
                line, control = control.split(b'\n', 1)
                cols, rows = map(int, line.split())
                resize(master, cols, rows)
    sys.exit(child.wait())


main()
