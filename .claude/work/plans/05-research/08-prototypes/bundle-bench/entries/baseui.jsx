import {createRoot} from 'react-dom/client'; import React from 'react';
import {Dialog} from '@base-ui/react/dialog'; import {Menu} from '@base-ui/react/menu'; import {Select} from '@base-ui/react/select'; import {Tooltip} from '@base-ui/react/tooltip'; import {Tabs} from '@base-ui/react/tabs'; import {Popover} from '@base-ui/react/popover'; import {Collapsible} from '@base-ui/react/collapsible'; import {ScrollArea} from '@base-ui/react/scroll-area';
const A=()=><Tooltip.Provider><Dialog.Root><Dialog.Trigger>x</Dialog.Trigger><Dialog.Portal><Dialog.Popup>y</Dialog.Popup></Dialog.Portal></Dialog.Root>
<Menu.Root><Menu.Trigger>m</Menu.Trigger><Menu.Portal><Menu.Positioner><Menu.Popup><Menu.Item>i</Menu.Item></Menu.Popup></Menu.Positioner></Menu.Portal></Menu.Root>
<Select.Root><Select.Trigger/><Select.Portal><Select.Positioner><Select.Popup><Select.Item value="a">a</Select.Item></Select.Popup></Select.Positioner></Select.Portal></Select.Root>
<Tabs.Root><Tabs.List><Tabs.Tab value="a">a</Tabs.Tab></Tabs.List></Tabs.Root><Popover.Root/><Collapsible.Root/><ScrollArea.Root/></Tooltip.Provider>;
createRoot(document.getElementById('r')).render(<A/>);
