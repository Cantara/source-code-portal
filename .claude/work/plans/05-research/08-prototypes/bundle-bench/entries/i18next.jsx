import {createRoot} from 'react-dom/client'; import React from 'react'; import i18n from 'i18next'; import {initReactI18next,useTranslation} from 'react-i18next';
i18n.use(initReactI18next).init({lng:'en',resources:{en:{translation:{hello:'Hello {{name}}',n_one:'{{count}} repo',n_other:'{{count}} repos'}}}});
function A(){const {t}=useTranslation(); return <div>{t('hello',{name:'x'})}{t('n',{count:2})}</div>}
createRoot(document.getElementById('r')).render(<A/>);
