import { render } from 'lit';
import { describe, expect, it } from 'vitest';
import { decodeEntities, headingsOf, parseDocHtml, renderDocNode, safeDocHref, type DocElement } from './docs-render';

const sample = `<h1 id="ssh-schluessel">SSH-Schlüssel</h1>
<p>Erst <strong>lesen</strong>, dann <code>ssh-keygen -C &#34;a@b&#34;</code> &amp; fertig.</p>
<h2 id="linux">Linux &lt;und&gt; macOS</h2>
<ul>
<li>Punkt <a href="/#/doku/ssh-2fa">2FA</a></li>
<li><a href="https://example.com/x" target="_blank" rel="noopener noreferrer">außen</a></li>
</ul>
<h3 id="windows">Windows</h3>
<pre><code class="language-bash">echo &#39;x&#39; &lt;script&gt;</code></pre>
<div class="table-responsive"><table><thead><tr><th>A</th></tr></thead><tbody><tr><td>1</td></tr></tbody></table></div>`;

describe('parseDocHtml', () => {
  const tree = parseDocHtml(sample);

  it('keeps headings with their ids and decodes text', () => {
    const h2 = tree.find((node) => typeof node !== 'string' && node.tag === 'h2') as DocElement;
    expect(h2.attrs.id).toBe('linux');
    expect(h2.children).toEqual(['Linux <und> macOS']);
  });

  it('rewrites old doc links and marks external ones', () => {
    expect(headingsOf(tree)).toEqual([
      { id: 'linux', text: 'Linux <und> macOS', level: 2 },
      { id: 'windows', text: 'Windows', level: 3 },
    ]);
    const container = document.createElement('div');
    render(tree.map((node) => renderDocNode(node, { anchorLabel: 'Link' })), container);
    const links = [...container.querySelectorAll('li a')];
    expect(links[0]!.getAttribute('href')).toBe('/docs/ssh-2fa');
    expect(links[1]!.getAttribute('target')).toBe('_blank');
    expect(links[1]!.getAttribute('rel')).toBe('noopener noreferrer');
  });

  it('renders escaped markup as text, never as elements', () => {
    const container = document.createElement('div');
    render(tree.map((node) => renderDocNode(node, { anchorLabel: 'Link' })), container);
    expect(container.querySelector('script')).toBeNull();
    expect(container.querySelector('pre code')!.textContent).toBe("echo 'x' <script>");
    expect(container.querySelector('table td')!.textContent).toBe('1');
  });

  it('drops unknown tags and attributes but keeps their text', () => {
    const nodes = parseDocHtml('<p onclick="x()">a<img src="x" onerror="y()">b<span style="c">c</span></p>');
    expect(nodes).toEqual([{ tag: 'p', attrs: {}, children: ['a', 'b', 'c'] }]);
  });

  it('turns unsafe links into plain text', () => {
    const container = document.createElement('div');
    const nodes = parseDocHtml('<p><a href="javascript:alert(1)">klick</a></p>');
    render(nodes.map((node) => renderDocNode(node, { anchorLabel: 'Link' })), container);
    expect(container.querySelector('a')).toBeNull();
    expect(container.textContent).toBe('klick');
  });

  it('makes repeated heading ids unique, the first one keeps its id', () => {
    const nodes = parseDocHtml('<h3 id="key">A</h3><h3 id="key">B</h3><h3 id="key">C</h3>');
    expect(headingsOf(nodes).map((heading) => heading.id)).toEqual(['key', 'key-2', 'key-3']);
  });

  it('survives unbalanced markup', () => {
    expect(parseDocHtml('<ul><li>a</ul></li><p>b')).toEqual([
      { tag: 'ul', attrs: {}, children: [{ tag: 'li', attrs: {}, children: ['a'] }] },
      { tag: 'p', attrs: {}, children: ['b'] },
    ]);
  });
});

describe('safeDocHref', () => {
  it('accepts anchors, internal paths and http(s)', () => {
    expect(safeDocHref('#abschnitt')).toEqual({ href: '#abschnitt', external: false });
    expect(safeDocHref('/doku/agent#install')).toEqual({ href: '/docs/agent#install', external: false });
    expect(safeDocHref('/settings/general')).toEqual({ href: '/settings/general', external: false });
    expect(safeDocHref('https://repo.techeve.de')).toEqual({ href: 'https://repo.techeve.de', external: true });
  });

  it('rejects scripts, protocol-relative and relative targets', () => {
    for (const raw of ['javascript:alert(1)', 'data:text/html,x', '//evil.example', 'relative/path', '/\\evil']) {
      expect(safeDocHref(raw)).toBeNull();
    }
  });
});

describe('decodeEntities', () => {
  it('decodes the entities of html.EscapeString and numeric ones', () => {
    expect(decodeEntities('&lt;a&gt; &amp; &#34;b&#34; &#39;c&#39; &#x41;')).toBe('<a> & "b" \'c\' A');
  });
});
