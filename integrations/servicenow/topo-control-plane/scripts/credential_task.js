(function process(request, response) {
    'use strict';
    var result = new TopoControlPlane().credential(String(request.pathParams.id || ''), request.body.data || {});
    response.setHeader('Cache-Control', 'no-store');
    response.setHeader('Pragma', 'no-cache');
    response.setStatus(result.status);
    response.setBody(result.body);
})(request, response);
