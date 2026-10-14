import {createRoot} from 'react-dom/client'; import React from 'react'; import {i18n} from '@lingui/core'; import {I18nProvider,useLingui} from '@lingui/react';
i18n.load('en',{hello:'Hello {name}', n:'{count, plural, one {# repo} other {# repos}}'}); i18n.activate('en');
function A(){const {i18n}=useLingui(); return <div>{i18n._('hello',{name:'x'})}{i18n._('n',{count:2})}</div>}
createRoot(document.getElementById('r')).render(<I18nProvider i18n={i18n}><A/></I18nProvider>);
