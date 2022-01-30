const path = require('path');

module.exports = {
  entry: './term.js',
  output: {
    path: path.resolve(__dirname, 'dist'),
    filename: 'main.js',
  },
};
