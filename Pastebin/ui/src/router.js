import { createRouter, createWebHistory } from 'vue-router';
import FileUpload from './components/FileUpload.vue';
import RetrievePage from './components/RetrievePage.vue'; // Create this component

const routes = [
    { path: '/', component: FileUpload },
    //{ path: '/upload', component: FileUpload },
    { path: '/abc', component: RetrievePage }
];
const router = createRouter({
    history: createWebHistory(),
    routes
});

export default router;