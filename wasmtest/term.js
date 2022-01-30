import { Terminal } from 'xterm';
import { FitAddon } from 'xterm-addon-fit';

const term = new Terminal({
  fontFamily: '"Cascadia Code", Menlo, monospace',
  // theme: baseTheme,
  cursorBlink: true
});
const fitAddon = new FitAddon();
term.loadAddon(fitAddon);

let is_open = false
window.startTerminal = function () {
  if (is_open) {
    return;
  }
  is_open = true

  term.open(document.querySelector('.term .inner'));
  term.focus();
  fitAddon.fit();


  // TODO: Use a nicer default font
  term.clear();
}

window.theTerminal = term;
term._inSSH = false;

term.onData(e => {
  if (term._inSSH) {
    sendSSHInput(e);
    return
  }
});