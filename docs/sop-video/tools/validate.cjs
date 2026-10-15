'use strict';
const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '..');

function validate(project, exists = file => fs.existsSync(path.join(root, file))) {
  const errors = [];
  if (project.schema_version !== 1) errors.push('Unsupported schema_version');
  if (!Array.isArray(project.segments) || !Array.isArray(project.chapters)) errors.push('segments and chapters must be arrays');
  const segments = new Map();
  for (const segment of project.segments || []) {
    if (!/^[a-z][a-z0-9_]*$/.test(segment.id || '')) errors.push(`Invalid segment id: ${segment.id}`);
    if (segments.has(segment.id)) errors.push(`Duplicate segment id: ${segment.id}`);
    if (!['pending', 'ready'].includes(segment.status)) errors.push(`Invalid status for ${segment.id}`);
    if (segment.file !== `media/${segment.id.replaceAll('_', '-')}.webm`) errors.push(`Unexpected media path for ${segment.id}`);
    if (segment.status === 'ready' && !exists(segment.file)) errors.push(`Ready clip missing: ${segment.file}`);
    segments.set(segment.id, segment);
  }
  const chapterIds = new Set();
  for (const chapter of project.chapters || []) {
    if (!/^[a-z][a-z0-9_]*$/.test(chapter.id || '')) errors.push(`Invalid chapter id: ${chapter.id}`);
    if (chapterIds.has(chapter.id)) errors.push(`Duplicate chapter id: ${chapter.id}`);
    chapterIds.add(chapter.id);
    if (!chapter.start || !chapter.end || !chapter.actor) errors.push(`Chapter ${chapter.id} needs start, end and actor`);
    if (!Array.isArray(chapter.segments) || chapter.segments.length === 0) errors.push(`Chapter ${chapter.id} has no segments`);
    for (const id of chapter.segments || []) if (!segments.has(id)) errors.push(`Chapter ${chapter.id} refers to unknown segment ${id}`);
  }
  return errors;
}

if (require.main === module) {
  const project = JSON.parse(fs.readFileSync(path.join(root, 'project.json'), 'utf8'));
  const errors = validate(project);
  for (const error of errors) console.error(error);
  if (errors.length) process.exitCode = 1;
  else console.log(`${project.chapters.length} chapters, ${project.segments.length} replaceable segments; ${project.segments.filter(s => s.status === 'ready').length} ready.`);
}
module.exports = { validate };
