import createClient from 'openapi-fetch'; const c=createClient({baseUrl:'/api/v1'}); c.GET('/x');
