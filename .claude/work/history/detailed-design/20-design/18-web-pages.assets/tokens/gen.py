from tokens import *
from viz import VL,VD,AV
def o(v): L,C,h=v; return f'oklch({L:g} {C:g} {h:g})'
def block(d,viz):
    m=dict(d)
    lines=[]
    def a(k,v): lines.append(f'  --{k}: {v};')
    a('background',o(m['background'])); a('foreground',o(m['foreground']))
    a('card',o(m['card'])); a('card-foreground',o(m['card-foreground']))
    a('popover',o(m['card'])); a('popover-foreground',o(m['card-foreground']))
    a('primary',o(m['primary'])); a('primary-foreground',o(m['primary-foreground']))
    a('secondary',o(m['muted'])); a('secondary-foreground',o(m['foreground']))
    a('muted',o(m['muted'])); a('muted-foreground',o(m['muted-foreground']))
    a('accent',o(m['accent'])); a('accent-foreground',o(m['accent-foreground']))
    a('destructive',o(m['danger']))
    a('border',o(m['border'])); a('input',o(m['input'])); a('ring',o(m['ring']))
    a('subtle',o(m['subtle'])); a('link',o(m['link']))
    for k in ('success','warning','danger','info'):
        a(k,o(m[k])); a(k+'-bg',o(m[k+'-bg']))
    a('sidebar',o(m['sidebar'])); a('sidebar-foreground',o(m['foreground']))
    a('sidebar-primary',o(m['primary'])); a('sidebar-primary-foreground',o(m['primary-foreground']))
    a('sidebar-accent',o(m['accent'])); a('sidebar-accent-foreground',o(m['accent-foreground']))
    a('sidebar-border',o(m['border'])); a('sidebar-ring',o(m['ring']))
    for i,v in enumerate(viz,1): a(f'viz-{i}',o(v))
    for i in range(1,6): a(f'chart-{i}',f'var(--viz-{i})')
    return '\n'.join(lines)
css=f'''/* web/src/styles/tokens.css : design tokens of the SCP SPA (unit 18-web-pages). Generated once by tokens/gen.py from tokens/tokens.py;
   every foreground/background pair is verified by tokens/contrast.py (see contrast-report.txt) and by the Vitest contrast test. */
:root {{
{block(LIGHT,VL)}
  --radius: 0.5rem;
  --font-sans: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace;
  color-scheme: light;
}}
.dark {{
{block(DARK,VD)}
  color-scheme: dark;
}}
/* avatar backgrounds: white text >= 4.5:1 in both themes, so they are theme independent */
:root {{
''' + '\n'.join(f'  --avatar-{i}: {o(v)};' for i,v in enumerate(AV,1)) + '''
}
'''
open('tokens.css','w').write(css)
out=[]; b=run('light',LIGHT,out)+run('dark',DARK,out)
open('contrast-report.txt','w').write('\n'.join(out)+f'\nFAILS {b}\n')
print(css[:600]); print(b)
