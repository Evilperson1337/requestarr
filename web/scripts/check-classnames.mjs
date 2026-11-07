// Every class name written as a string in web/src has a rule in the stylesheet, or is listed
// in classnameAllowed with the reason it needs none. An unstyled class renders as nothing, and
// neither the type checker nor the browser tests notice.
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

const classnameAllowed = new Map([
    // Marker classes that name an element and carry no styling of their own.
    ['brandMark', 'the logo svg; its size comes from attributes'],
    ['movieState', 'names the request and availability section'],
])

const walk = dir => readdirSync(dir).flatMap(name => {
    const path = join(dir, name)
    return statSync(path).isDirectory() ? walk(path) : [path]
})
const sourceFiles = walk('src').filter(path => /\.tsx?$/.test(path))
const styled = new Set()
for (const path of walk('src').filter(path => path.endsWith('.css'))) {
    for (const match of readFileSync(path, 'utf8').matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)) styled.add(match[1])
}

// The text of a className attribute: a quoted string, or the braces after it with nesting kept.
function attributeText(source, from) {
    if (source[from] === '"') return source.slice(from + 1, source.indexOf('"', from + 1))
    if (source[from] !== '{') return ''
    let depth = 0
    for (let i = from; i < source.length; i++) {
        if (source[i] === '{') depth++
        if (source[i] === '}' && --depth === 0) return source.slice(from + 1, i)
    }
    return ''
}

const missing = []
for (const path of sourceFiles.filter(path => path.endsWith('.tsx'))) {
    const source = readFileSync(path, 'utf8')
    for (const match of source.matchAll(/className=/g)) {
        const text = attributeText(source, match.index + match[0].length)
        // Only a plain literal is checked: className="a b" or className={'a b'}. An expression
        // (a condition, a lookup, a template with a hole) can hold names that are data, not classes.
        const literal = text.startsWith('"') ? null : /^\s*(?:'([^']*)'|`([^`$]*)`)\s*$/.exec(text)
        const quoted = match.input[match.index + match[0].length] === '"' ? [text] : literal ? [literal[1] ?? literal[2]] : []
        for (const group of quoted) {
            for (const name of group.split(/\s+/).filter(Boolean)) {
                if (!/^-?[_a-zA-Z][\w-]*$/.test(name) || styled.has(name) || classnameAllowed.has(name)) continue
                const line = source.slice(0, match.index).split('\n').length
                missing.push(`${path}:${line}: class "${name}" has no rule in the stylesheet`)
            }
        }
    }
}
if (missing.length) {
    console.error([...new Set(missing)].join('\n'))
    process.exit(1)
}
console.log('ok: every class name in web/src has a style rule')
