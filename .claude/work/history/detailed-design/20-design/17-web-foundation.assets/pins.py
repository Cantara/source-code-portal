import json, urllib.request, sys, re, concurrent.futures as cf
CUT = "2026-10-07T00:00:00"
pk = """react react-dom @types/react @types/react-dom typescript @typescript/typescript6 @typescript/native vite @vitejs/plugin-react @rolldown/plugin-babel babel-plugin-react-compiler @babel/core tailwindcss @tailwindcss/vite @tailwindcss/typography shadcn @base-ui/react cn class-variance-authority tw-animate-css lucide-react @primer/octicons-react @tanstack/react-router @tanstack/router-plugin @tanstack/virtual-file-routes @tanstack/react-query @tanstack/react-table @tanstack/react-virtual zod recharts cmdk sigma graphology d3-force @dagrejs/dagre mermaid openapi-typescript openapi-fetch @redocly/cli vitest @vitest/browser-playwright vitest-browser-react jsdom @testing-library/react @testing-library/dom @testing-library/user-event @testing-library/jest-dom msw @playwright/test playwright axe-core @axe-core/playwright eslint @eslint/js typescript-eslint eslint-plugin-react-hooks eslint-plugin-jsx-a11y eslint-plugin-boundaries eslint-import-resolver-typescript eslint-plugin-import-x eslint-plugin-react-refresh prettier prettier-plugin-tailwindcss @lingui/core @lingui/react @lingui/cli @lingui/vite-plugin @lingui/babel-plugin-lingui-macro @lingui/conf eslint-plugin-lingui size-limit @size-limit/file lighthouse knip @fontsource-variable/geist @types/node @types/trusted-types globals pnpm @storybook/react-vite storybook msw-storybook-addon openapi-msw @vitest/coverage-v8 @tanstack/react-router-devtools @tanstack/react-query-devtools react-hook-form @hookform/resolvers vite-plugin-compression2 @tanstack/eslint-plugin-query @tanstack/eslint-plugin-router @biomejs/biome @eslint-react/eslint-plugin""".split()
def get(p):
    try:
        u="https://registry.npmjs.org/"+p.replace("/","%2F")
        d=json.load(urllib.request.urlopen(urllib.request.Request(u,headers={"Accept":"application/json"}),timeout=30))
    except Exception as e:
        return p,None,str(e)
    lat=d["dist-tags"].get("latest"); t=d["time"]
    major=lat.split(".")[0]
    cands=[(t[v],v) for v in d["versions"] if re.fullmatch(r"\d+\.\d+\.\d+",v) and v.split(".")[0]==major and t.get(v,"9")<CUT]
    cands.sort()
    old=cands[-1] if cands else None
    lic=d["versions"][lat].get("license")
    eng=d["versions"][lat].get("engines")
    return p,(lat,t[lat][:10],old[1] if old else None, old[0][:10] if old else None,lic,eng),None
with cf.ThreadPoolExecutor(8) as ex:
    for p,r,e in ex.map(get,pk):
        print(p, r if r else "ERR "+str(e))
