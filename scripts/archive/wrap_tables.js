const fs = require('fs');
let c = fs.readFileSync('public/app.js', 'utf8');
let modified = false;

let result = '';
let lastIndex = 0;
const tableRe = /<table>/g;
let match;

while ((match = tableRe.exec(c)) !== null) {
    const idx = match.index;
    const before = c.substring(Math.max(0, idx - 50), idx);
    
    result += c.substring(lastIndex, idx);
    
    if (!before.includes('table-wrapper')) {
        result += '<div class="table-wrapper">\n    <table>';
        modified = true;
    } else {
        result += '<table>';
    }
    
    lastIndex = idx + 7;
}
result += c.substring(lastIndex);
c = result;

result = '';
lastIndex = 0;
const endTableRe = /<\/table>/g;
while ((match = endTableRe.exec(c)) !== null) {
    const idx = match.index;
    const after = c.substring(idx + 8, Math.min(c.length, idx + 50));
    
    result += c.substring(lastIndex, idx);
    
    // We only want to add </div> if the immediate next tag is NOT </div>.
    // Sometimes it might be `</table></div>`, or `</table>\n</div>`.
    if (!after.match(/^\s*<\/div>/)) {
        result += '</table></div>';
        modified = true;
    } else {
        result += '</table>';
    }
    
    lastIndex = idx + 8;
}
result += c.substring(lastIndex);

fs.writeFileSync('public/app.js', result);
console.log('Modified:', modified);
