// Global Fix Script: Nischoy Topo — Register Discovery Source.
// Keep Unloadable disabled: never capture a customer's whole choice list.
(function () {
    var choice = new GlideRecord('sys_choice');
    choice.addQuery('name', 'cmdb_ci');
    choice.addQuery('element', 'discovery_source');
    choice.addQuery('value', 'Nischoy Topo');
    choice.addQuery('language', 'en');
    choice.addQuery('dependent_value', '');
    choice.addQuery('sys_domain', 'global');
    choice.setLimit(2);
    choice.query();
    if (choice.next()) {
        var inactive = choice.getValue('inactive');
        if (choice.next()) {
            throw new Error('Nischoy Topo: duplicate English discovery-source choices; ask an administrator to resolve them.');
        }
        if (inactive !== 'false' && inactive !== '0') {
            throw new Error('Nischoy Topo: discovery-source choice is inactive; ask an administrator to review it.');
        }
        gs.info('Nischoy Topo: discovery source already registered; existing choice preserved.');
        return;
    }
    choice.initialize();
    choice.setValue('name', 'cmdb_ci');
    choice.setValue('element', 'discovery_source');
    choice.setValue('value', 'Nischoy Topo');
    choice.setValue('label', 'Nischoy Topo');
    choice.setValue('language', 'en');
    choice.setValue('dependent_value', '');
    choice.setValue('sys_domain', 'global');
    choice.setValue('inactive', false);
    if (!choice.insert()) {
        throw new Error('Nischoy Topo: could not register discovery source.');
    }
    gs.info('Nischoy Topo: discovery source registered.');
}());
