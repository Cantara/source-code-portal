import { SafeHtml } from "@/safe-html"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogTrigger, DialogTitle, DialogDescription } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"
import { Bar, BarChart, XAxis } from "recharts"
import { Command, CommandInput, CommandList, CommandItem, CommandEmpty } from "@/components/ui/command"
import { Toaster, toast } from "@/components/ui/toast"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { Sheet, SheetContent, SheetTrigger, SheetTitle, SheetDescription } from "@/components/ui/sheet"

const HTML = '<p id="sh1">hello <a href="/x">link</a></p>'
const cfg = { v: { label: "Commits", color: "var(--chart-1)" } } satisfies ChartConfig
const data = [{ d: "Mon", v: 3 }, { d: "Tue", v: 7 }, { d: "Wed", v: 5 }]

export function App() {
  return (
    <TooltipProvider>
      <div className="flex flex-col gap-4 p-6" id="app">
        <h1 className="text-xl font-medium">CSP spike</h1><SafeHtml html={HTML} /><div className="prose dark:prose-invert"><table><tr><td>a</td></tr></table><pre><code>x</code></pre><blockquote>q</blockquote><kbd>k</kbd><details><summary>s</summary>d</details><ul className="contains-task-list"><li><input type="checkbox"/>t</li></ul></div>
        <Dialog>
          <DialogTrigger render={<Button id="dlg">Open dialog</Button>} />
          <DialogContent><DialogTitle>T</DialogTitle><DialogDescription>D</DialogDescription></DialogContent>
        </Dialog>
        <ScrollArea className="h-24 w-48 border"><div className="h-96">tall</div></ScrollArea>
        <Select defaultValue="a"><SelectTrigger id="sel"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="a">A</SelectItem><SelectItem value="b">B</SelectItem></SelectContent></Select>
        <Tooltip><TooltipTrigger render={<Button id="tip" variant="outline">Hover</Button>} /><TooltipContent>Tip</TooltipContent></Tooltip>
        <DropdownMenu><DropdownMenuTrigger render={<Button id="dd">Menu</Button>} /><DropdownMenuContent><DropdownMenuItem>One</DropdownMenuItem></DropdownMenuContent></DropdownMenu>
        <Tabs defaultValue="a"><TabsList><TabsTrigger value="a">A</TabsTrigger><TabsTrigger value="b">B</TabsTrigger></TabsList><TabsContent value="a">pa</TabsContent></Tabs>
        <ChartContainer config={cfg} className="h-40 w-80">
          <BarChart data={data} accessibilityLayer><XAxis dataKey="d" /><ChartTooltip content={<ChartTooltipContent />} /><Bar dataKey="v" fill="var(--color-v)" /></BarChart>
        </ChartContainer>
        <Command className="w-64 border"><CommandInput placeholder="Search" /><CommandList><CommandEmpty>None</CommandEmpty><CommandItem>Repo A</CommandItem></CommandList></Command>
        <Popover><PopoverTrigger render={<Button id="pop">Pop</Button>} /><PopoverContent>pc</PopoverContent></Popover>
        <Sheet><SheetTrigger render={<Button id="sh">Sheet</Button>} /><SheetContent><SheetTitle>S</SheetTitle><SheetDescription>d</SheetDescription></SheetContent></Sheet>
        <Button id="toast" onClick={() => toast.add({ title: "Hello" })}>Toast</Button>
        <Toaster />
      </div>
    </TooltipProvider>
  )
}
export default App
