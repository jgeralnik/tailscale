// Hacked up version of https://xtermjs.org/js/demo.js
// for now.

import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';

export const term = new Terminal({
  fontFamily: '"Cascadia Code", Menlo, monospace',
  // theme: baseTheme,
  cursorBlink: true
});
const fitAddon = new FitAddon();
term.loadAddon(fitAddon);
term.open(document.querySelector('.term .inner'));
term.focus();
fitAddon.fit();

window.theTerminal = term;
term._inSSH = false;

term.prompt = () => {
  term.write('\r\n$ ');
};

// TODO: Use a nicer default font
term.writeln('Tailscale js/wasm demo; try running `help`.');
term.prompt();

term.onData(e => {
  if (term._inSSH) {
    sendSSHInput(e);
    return
  }
  switch (e) {
    case '\u0003': // Ctrl+C
      term.write('^C');
      term.prompt();
      break;
    case '\r': // Enter
      runCommand(term, command);
      command = '';
      break;
    case '\u007F': // Backspace (DEL)
      // Do not delete the prompt
      if (term._core.buffer.x > 2) {
        term.write('\b \b');
        if (command.length > 0) {
          command = command.substr(0, command.length - 1);
        }
      }
      break;
    default: // Print all other characters for demo
      if (e >= String.fromCharCode(0x20) && e <= String.fromCharCode(0x7B) || e >= '\u00a0') {
        command += e;
        term.write(e);
      }
  }
});

var command = '';
var commands = {
  help: {
    f: () => {
      term.writeln([
        'Welcome to Tailscale js/wasm! Try some of the commands below.',
        '',
        ...Object.keys(commands).map(e => `  ${e.padEnd(10)} ${commands[e].description}`)
      ].join('\n\r'));
      term.prompt();
    },
    description: 'Prints this help message',
  },
  tailscale: {
    f: (line) => {
      //term.writeln("TODO(bradfitz): run the tailscale command: "+line);
      runTailscaleCLI(line, function () { term.prompt() });
    },
    description: 'run cmd/tailscale'
  },
  http: {
    f: (line) => {
      runFakeCURL(line, function () { term.prompt() });
    },
    description: 'fetch a URL'
  },
  ssh: {
    f: (line) => {
      runSSH(line, function () { term.prompt() });
    },
    description: 'SSH to host'
  },
  goroutines: {
    f: () => {
      seeGoroutines();
    },
    description: 'dump goroutines'
  }
};

function runCommand(term, text) {
  const command = text.trim().split(' ')[0];
  if (command.length > 0) {
    term.writeln('');
    if (command in commands) {
      commands[command].f(text);
      return;
    }
    term.writeln(`${command}: command not found`);
  }
  term.prompt();
}

runFakeTerminal();