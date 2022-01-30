const path = require('path');

module.exports = {
  entry: './term.js',
  output: {
    path: path.resolve(__dirname, 'dist'),
    filename: 'main.js',
  },
  mode: "production",
  performance: {
    maxEntrypointSize: 500000,
    maxAssetSize: 500000
  }
};
