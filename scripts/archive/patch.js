const fs = require('fs');
let c = fs.readFileSync('public/app.js', 'utf8');

c = c.replace(
  /\['menu-stores', 'menu-inventory', 'menu-transfers', 'menu-users', 'menu-roles', 'menu-prefix-configs', 'menu-dynamic-labels', 'menu-audit-logs'\].forEach/,
  "document.getElementById('menu-stores').addEventListener('click', (e) => { e.preventDefault(); setActiveMenu('menu-stores'); closeSubmenus(); currentDoctype = 'Stores'; currentSearchQuery = ''; currentTablePage = 1; renderView('doctype-table'); });\n\n  ['menu-inventory', 'menu-transfers', 'menu-users', 'menu-roles', 'menu-prefix-configs', 'menu-dynamic-labels', 'menu-audit-logs'].forEach"
);

c = c.replace(
  /    <div class="table-wrapper" style="flex:1; overflow-y:auto;">\r?\n    <div class="table-wrapper">\r?\n    <table>/,
  '    <div class="table-wrapper" style="flex:1; overflow-y:auto;">\n    <table>'
);

c = c.replace(
  /  title\.textContent = `New \$\{getTranslatedLabel\(currentDoctype\)\}`;[\s\S]*?body\.innerHTML = '';[\s\S]*?for \(const f of state\.activeDocFields\) \{/,
  "  title.textContent = `New ${getTranslatedLabel(currentDoctype)}`;\n  body.innerHTML = '';\n\n  const activeDoc = state.activeDoctypes.find(d => d.name === currentDoctype);\n  const isMaster = activeDoc && activeDoc.document_type === 'Master';\n\n  for (const f of state.activeDocFields) {"
);

c = c.replace(
  /    const fg = document\.createElement\('div'\);\s*fg\.className = 'form-group';\s*fg\.innerHTML = `<label class="form-label">\$\{getTranslatedLabel\(f\.label\)\}\$\{f\.mandatory \? '<span class="required">\*<\/span>' : ''\}<\/label>`;/,
  "    const isCodeField = isMaster && f.fieldname.toLowerCase() === 'code';\n\n    const fg = document.createElement('div');\n    fg.className = 'form-group';\n    fg.innerHTML = `<label class=\"form-label\">${getTranslatedLabel(f.label)}${f.mandatory && !isCodeField ? '<span class=\"required\">*</span>' : ''}</label>`;"
);

c = c.replace(
  /    \} else \{\s*const input = document\.createElement\('input'\);\s*input\.className = 'form-input';\s*input\.type = 'text';\s*input\.name = f\.fieldname;\s*input\.required = f\.mandatory;\s*fg\.appendChild\(input\);\s*\}/,
  "    } else {\n      const input = document.createElement('input');\n      input.className = 'form-input';\n      input.type = 'text';\n      input.name = f.fieldname;\n      if (isCodeField) {\n        input.placeholder = 'Auto-generated upon save';\n        input.readOnly = true;\n        input.required = false;\n      } else {\n        input.required = f.mandatory;\n      }\n      fg.appendChild(input);\n    }"
);

c = c.replace(
  /  const form = document\.getElementById\('dynamic-modal-form'\);\s*const payload = \{\};\s*state\.activeDocFields\.forEach\(f => \{\s*if \(f\.fieldname === 'id' \|\| f\.fieldname === 'status'\) return;\s*const input = form\.querySelector\(`\[name="\$\{f\.fieldname\}"\]`\);\s*if \(input\) \{\s*if \(f\.fieldtype === 'Number'\) \{\s*payload\[f\.fieldname\] = parseFloat\(input\.value\);\s*\} else \{\s*payload\[f\.fieldname\] = input\.value;\s*\}\s*\}\s*\}\);\s*const res = await apiFetch\(`\/api\/v1\/doc\/\$\{currentDoctype\}`/,
  "  const form = document.getElementById('dynamic-modal-form');\n  const payload = {};\n  \n  const activeDoc = state.activeDoctypes.find(d => d.name === currentDoctype);\n  const isMaster = activeDoc && activeDoc.document_type === 'Master';\n  let codeFieldname = null;\n\n  state.activeDocFields.forEach(f => {\n    if (f.fieldname === 'id' || f.fieldname === 'status') return;\n    const isCodeField = isMaster && f.fieldname.toLowerCase() === 'code';\n    const input = form.querySelector(`[name=\"${f.fieldname}\"]`);\n    if (input) {\n      if (isCodeField && !input.value) {\n        codeFieldname = f.fieldname;\n      } else {\n        if (f.fieldtype === 'Number') {\n          payload[f.fieldname] = parseFloat(input.value);\n        } else {\n          payload[f.fieldname] = input.value;\n        }\n      }\n    }\n  });\n\n  if (codeFieldname) {\n    const seqRes = await apiFetch('/api/v1/sequence', {\n      method: 'POST',\n      body: JSON.stringify({\n        doc_type: currentDoctype,\n        store_code: 'HQ',\n        financial_year: new Date().getFullYear().toString()\n      })\n    });\n    if (seqRes && seqRes.ok) {\n      const seqData = await seqRes.json();\n      payload[codeFieldname] = seqData.code;\n    } else {\n      await showApiError(seqRes, 'Failed to generate Code sequence.');\n      return;\n    }\n  }\n\n  const res = await apiFetch(`/api/v1/doc/${currentDoctype}`"
);

fs.writeFileSync('public/app.js', c);
console.log('Patch complete.');
