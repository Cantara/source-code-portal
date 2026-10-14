import {createRoot} from 'react-dom/client'; import React from 'react';
import {Dialog,DropdownMenu,Select,Tooltip,Tabs,Popover,Collapsible,ScrollArea,Slot} from 'radix-ui';
const A=()=><Tooltip.Provider><Dialog.Root><Dialog.Trigger>x</Dialog.Trigger><Dialog.Portal><Dialog.Content>y</Dialog.Content></Dialog.Portal></Dialog.Root>
<DropdownMenu.Root><DropdownMenu.Trigger>m</DropdownMenu.Trigger><DropdownMenu.Content><DropdownMenu.Item>i</DropdownMenu.Item></DropdownMenu.Content></DropdownMenu.Root>
<Select.Root><Select.Trigger/><Select.Content><Select.Item value="a">a</Select.Item></Select.Content></Select.Root>
<Tabs.Root><Tabs.List><Tabs.Trigger value="a">a</Tabs.Trigger></Tabs.List></Tabs.Root><Popover.Root/><Collapsible.Root/><ScrollArea.Root/></Tooltip.Provider>;
createRoot(document.getElementById('r')).render(<A/>);
